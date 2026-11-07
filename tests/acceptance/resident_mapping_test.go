package acceptance

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"
)

type residentMapping struct {
	ID, Kind, Permissions                                                 string
	SizeBytes, RSSBytes, AnonymousBytes, LazyFreeBytes, PrivateDirtyBytes uint64
}

type residentMappingWindow struct {
	At       time.Time
	Mappings []residentMapping
}

// Only mapping identity/permissions/class and numeric counters leave procfs.
// No pathname, command line, memory contents or audio bytes are emitted.
const residentMappingProbe = `set -eu
case "$1" in ''|*[!0-9]*) exit 1;; esac
while read -r key a b c d rest; do
 case "$key" in
  *-*)
   case "$rest" in
    '[heap]') kind=heap;;
    '[stack]'|'[stack:'*) kind=stack;;
    *) if [ "$d" = 0 ]; then kind=anonymous; else kind=file-or-device; fi;;
   esac
   printf 'map %s %s %s\n' "$key" "$a" "$kind"
  ;;
  Size:|Rss:|Anonymous:|LazyFree:|Private_Dirty:) printf '%s %s %s\n' "$key" "$a" "$b";;
 esac
done < "/proc/$1/smaps"
`

func parseResidentMappings(data []byte) ([]residentMapping, error) {
	unknown := errors.New("owned numeric memory mappings unavailable")
	if len(data) == 0 || len(data) > dockerOutputLimit {
		return nil, unknown
	}
	var result []residentMapping
	var point *residentMapping
	var seen map[string]bool
	complete := func() bool {
		return point != nil && seen["Size:"] && seen["Rss:"] && seen["Anonymous:"] && seen["LazyFree:"] && seen["Private_Dirty:"] && point.AnonymousBytes <= point.RSSBytes && point.LazyFreeBytes <= point.AnonymousBytes && point.RSSBytes <= point.SizeBytes
	}
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) == 4 && fields[0] == "map" {
			if point != nil && !complete() {
				return nil, unknown
			}
			addresses := strings.Split(fields[1], "-")
			if len(addresses) != 2 {
				return nil, unknown
			}
			start, e1 := strconv.ParseUint(addresses[0], 16, 64)
			end, e2 := strconv.ParseUint(addresses[1], 16, 64)
			if e1 != nil || e2 != nil || end <= start || len(fields[2]) != 4 {
				return nil, unknown
			}
			switch fields[3] {
			case "heap", "stack", "anonymous", "file-or-device":
			default:
				return nil, unknown
			}
			digest := sha256.Sum256([]byte(fields[1]))
			result = append(result, residentMapping{ID: hex.EncodeToString(digest[:]), Kind: fields[3], Permissions: fields[2]})
			point = &result[len(result)-1]
			seen = make(map[string]bool)
			continue
		}
		if point == nil || len(fields) != 3 || fields[2] != "kB" || seen[fields[0]] {
			return nil, unknown
		}
		value, err := strconv.ParseUint(fields[1], 10, 64)
		if err != nil || value > ^uint64(0)/1024 {
			return nil, unknown
		}
		value *= 1024
		switch fields[0] {
		case "Size:":
			point.SizeBytes = value
		case "Rss:":
			point.RSSBytes = value
		case "Anonymous:":
			point.AnonymousBytes = value
		case "LazyFree:":
			point.LazyFreeBytes = value
		case "Private_Dirty:":
			point.PrivateDirtyBytes = value
		default:
			return nil, unknown
		}
		seen[fields[0]] = true
	}
	if scanner.Err() != nil || !complete() {
		return nil, unknown
	}
	return result, nil
}

func observeResidentMappings(ctx context.Context, e *acceptanceEnvironment, expected DockerSnapshot) (residentMappingWindow, error) {
	var result residentMappingWindow
	if expected.NativeNamespacePID == nil {
		return result, errors.New("native mapping owner unavailable")
	}
	containers, err := e.target.Containers(ctx)
	if err != nil {
		return result, err
	}
	data, err := e.target.run(ctx, "owned numeric native mappings", false, "exec", containers.BackendID, "sh", "-ec", residentMappingProbe, "p07-mappings", strconv.Itoa(*expected.NativeNamespacePID))
	if err != nil {
		return result, err
	}
	result.Mappings, err = parseResidentMappings(data)
	if err != nil {
		return residentMappingWindow{}, err
	}
	current, err := e.target.Observe(ctx)
	if err != nil || residentGenerationStable(expected, current) != nil {
		return residentMappingWindow{}, errors.New("native mapping process identity changed")
	}
	result.At = time.Now()
	return result, nil
}

func TestResidentMappingConsumerRejectsUnknownCountersAndPreservesReclaimable(t *testing.T) {
	valid := "map 1000-9000 rw-p anonymous\nSize: 32 kB\nRss: 16 kB\nAnonymous: 12 kB\nLazyFree: 4 kB\nPrivate_Dirty: 8 kB\n"
	points, err := parseResidentMappings([]byte(valid))
	if err != nil || len(points) != 1 || points[0].RSSBytes != 16*1024 || points[0].LazyFreeBytes != 4*1024 || strings.Contains(points[0].ID, "1000-9000") {
		t.Fatal("observed mapping lost numeric residency/reclaimability or retained an address")
	}
	for _, invalid := range []string{"", strings.Replace(valid, "LazyFree: 4 kB\n", "", 1), valid + "Rss: 16 kB\n", strings.Replace(valid, "Rss: 16 kB", "Rss: 16 MB", 1), strings.Replace(valid, "Rss: 16 kB", "Rss: 18446744073709551615 kB", 1), strings.Replace(valid, "Anonymous: 12 kB", "Anonymous: 20 kB", 1)} {
		if _, err := parseResidentMappings([]byte(invalid)); err == nil {
			t.Fatal("incomplete/invalid mapping became known zero or trusted residency")
		}
	}
}

func TestResidentMappingProbeObservesOwnedProcessWithoutPaths(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "sh", "-ec", residentMappingProbe, "p07-owned-mappings", strconv.Itoa(os.Getpid()))
	output := &dockerOutput{cancel: cancel, retain: true}
	cmd.Stdout, cmd.Stderr = output, output
	cmd.WaitDelay = 200 * time.Millisecond
	if err := cmd.Run(); err != nil || output.overflow {
		t.Fatal("owned mapping probe failed within its bound")
	}
	data := []byte(output.data.String())
	points, err := parseResidentMappings(data)
	if err != nil || len(points) == 0 || strings.Contains(string(data), "/") {
		t.Fatal("numeric mapping observation failed or exposed a pathname")
	}
}
