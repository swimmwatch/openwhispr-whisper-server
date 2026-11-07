package acceptance

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
)

// A parent owns the pair and workload. The P07 aggregate retains its PID 1
// and native semantics. Native descendants are measured separately. These
// serial observations are not stability or GPU evidence; decoder RSS remains
// outside the API aggregate. No private identity is included in the artifact.
type p08ResourceTarget struct {
	Kubeconfig       string `json:"kubeconfig"`
	Context          string `json:"context"`
	Namespace        string `json:"namespace"`
	NamespaceUID     string `json:"namespace_uid"`
	Release          string `json:"release"`
	APIPod           string `json:"api_pod"`
	APIUID           string `json:"api_pod_uid"`
	APIContainer     string `json:"api_container"`
	BackendPod       string `json:"backend_pod"`
	BackendUID       string `json:"backend_pod_uid"`
	BackendContainer string `json:"backend_container"`
	run              func(context.Context, ...string) ([]byte, error)
}

var p08Name = regexp.MustCompile(`^[a-z0-9]([-a-z0-9.]*[a-z0-9])?$`)

func (t p08ResourceTarget) valid() bool {
	if !filepath.IsAbs(t.Kubeconfig) || t.Context == "" || t.NamespaceUID == "" || t.APIUID == "" || t.BackendUID == "" || t.APIUID == t.BackendUID {
		return false
	}
	for _, n := range []string{t.Namespace, t.Release, t.APIPod, t.BackendPod, t.APIContainer, t.BackendContainer} {
		if len(n) > 253 || !p08Name.MatchString(n) {
			return false
		}
	}
	api := strings.LastIndexByte(t.APIPod, '-')
	backend := strings.LastIndexByte(t.BackendPod, '-')
	if api < 0 || backend < 0 || t.APIContainer != "api" || t.BackendContainer != "backend" {
		return false
	}
	ordinal := t.APIPod[api+1:]
	if ordinal == "" || ordinal != t.BackendPod[backend+1:] {
		return false
	}
	_, err := strconv.ParseUint(ordinal, 10, 32)
	return err == nil && t.APIPod != t.BackendPod
}
func (t p08ResourceTarget) command(ctx context.Context, args ...string) ([]byte, error) {
	if !t.valid() {
		return nil, errors.New("invalid private resource selection")
	}
	run := t.run
	if run == nil {
		run = kubectlOutput
	}
	return run(ctx, append([]string{"--kubeconfig", t.Kubeconfig, "--context", t.Context, "--namespace", t.Namespace, "--request-timeout=4s"}, args...)...)
}

type p08ContainerIdentity struct {
	ID       string
	Restarts int
}

func (t p08ResourceTarget) identity(ctx context.Context) ([2]p08ContainerIdentity, error) {
	var result [2]p08ContainerIdentity
	data, err := t.command(ctx, "get", "namespace", t.Namespace, "-o", "jsonpath={.metadata.uid}")
	if err != nil || string(data) != t.NamespaceUID {
		return result, errors.New("namespace identity unavailable")
	}
	for i, selection := range []struct{ role, pod, uid, container string }{{"api", t.APIPod, t.APIUID, t.APIContainer}, {"backend", t.BackendPod, t.BackendUID, t.BackendContainer}} {
		data, err = t.command(ctx, "get", "pod", selection.pod, "-o", "json")
		var pod struct {
			Metadata struct {
				UID               string            `json:"uid"`
				Labels            map[string]string `json:"labels"`
				DeletionTimestamp *string           `json:"deletionTimestamp"`
			} `json:"metadata"`
			Spec struct {
				HostPID bool `json:"hostPID"`
			} `json:"spec"`
			Status struct {
				ContainerStatuses []struct {
					Name         string `json:"name"`
					ContainerID  string `json:"containerID"`
					RestartCount *int   `json:"restartCount"`
					State        struct {
						Running *json.RawMessage `json:"running"`
					} `json:"state"`
				} `json:"containerStatuses"`
			} `json:"status"`
		}
		if err != nil || json.Unmarshal(data, &pod) != nil || pod.Metadata.UID != selection.uid || pod.Metadata.DeletionTimestamp != nil || pod.Spec.HostPID || pod.Metadata.Labels["app.kubernetes.io/instance"] != t.Release || pod.Metadata.Labels["app.kubernetes.io/component"] != selection.role {
			return result, errors.New("selected Pod identity unavailable")
		}
		found := 0
		for _, s := range pod.Status.ContainerStatuses {
			if s.Name == selection.container {
				if s.ContainerID == "" || s.RestartCount == nil || *s.RestartCount < 0 || s.State.Running == nil {
					return result, errors.New("selected container identity unavailable")
				}
				result[i] = p08ContainerIdentity{s.ContainerID, *s.RestartCount}
				found++
			}
		}
		if found != 1 {
			return result, errors.New("selected container identity unavailable")
		}
	}
	return result, nil
}

// Process start times distinguish replacement and PID reuse. Only numeric tokens
// leave this probe; neither command lines nor paths are reported.
const p08ProcessIdentityProbe = `set -eu
start() {
 IFS= read -r line < "/proc/$1/stat"
 rest=${line##*) }
 [ "$rest" != "$line" ] || exit 1
 set -- $rest
 [ "$#" -ge 20 ] || exit 1
 shift 19
 case "$1" in ''|*[!0-9]*) exit 1;; esac
 printf '%s' "$1"
}
native=0
if [ "$1" = backend ]; then
 for f in /proc/[0-9]*/comm; do
  IFS= read -r n < "$f" || continue
  if [ "$n" = whisper-server ]; then [ "$native" = 0 ] || exit 1; native=${f#/proc/}; native=${native%/comm}; fi
 done
 [ "$native" -gt 1 ] || exit 1
fi
printf '%s ' "$(start 1)"
if [ "$native" -gt 1 ]; then printf '%s %s\n' "$native" "$(start "$native")"; else printf '0 0\n'; fi
`

// This serial descendant measurement is separate from P07's unchanged aggregate.
// A disappearing process, missing smaps_rollup or changed tree fails closed.
const p08NativeDescendantProbe = `set -eu
proc=$1; owner=$2; expected=$3
` + dockerRSSProbe + `
processes() {
 for s in "$proc"/[0-9]*/stat; do
  [ -r "$s" ] || exit 1
  IFS= read -r line < "$s" || exit 1
  pid=${s#"$proc"/}; pid=${pid%/stat}
  rest=${line##*) }; [ "$rest" != "$line" ] || exit 1
  set -- $rest; [ "$#" -ge 20 ] || exit 1
  parent=$2; shift 19; start=$1
  case "$pid:$parent:$start" in *[!0-9:]*|'') exit 1;; esac
  printf '%s:%s:%s\n' "$pid" "$parent" "$start"
 done
}
select_processes() {
 all=$1; found=0
 for row in $all; do
  pid=${row%%:*}; tail=${row#*:}; parent=${tail%%:*}; start=${tail#*:}
  if [ "$pid" = "$owner" ]; then [ "$start" = "$expected" ] || exit 1; found=1; continue; fi
  p=$pid; steps=0
  while [ "$p" != 0 ]; do
   [ "$p" != "$$" ] || break
   if [ "$p" = "$owner" ]; then printf '%s:%s\n' "$pid" "$start"; break; fi
   next=
   for r in $all; do
    q=${r%%:*}; [ "$q" = "$p" ] || continue
    v=${r#*:}; next=${v%%:*}; break
   done
   [ -n "$next" ] || exit 1
   p=$next; steps=$((steps+1)); [ "$steps" -le 4096 ] || exit 1
  done
 done
 [ "$found" = 1 ] || exit 1
}
before=$(processes); selected=$(select_processes "$before")
for row in $selected; do
 pid=${row%%:*}; start=${row#*:}
 rss=$(rss_bytes_from_report < "$proc/$pid/smaps_rollup") || exit 1
 printf '%s:%s:%s\n' "$pid" "$start" "$rss"
done
after=$(processes); selected_after=$(select_processes "$after")
[ "$selected" = "$selected_after" ] || exit 1
printf 'OK\n'
`

func p08NativeDescendantRSS(data []byte) (uint64, error) {
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) == 0 || lines[len(lines)-1] != "OK" || len(lines) > 4097 {
		return 0, errors.New("native descendants unavailable")
	}
	seen := make(map[uint64]bool)
	var total uint64
	for _, line := range lines[:len(lines)-1] {
		f := strings.Split(line, ":")
		if len(f) != 3 {
			return 0, errors.New("native descendants unavailable")
		}
		pid, e1 := strconv.ParseUint(f[0], 10, 64)
		start, e2 := strconv.ParseUint(f[1], 10, 64)
		rss, e3 := strconv.ParseUint(f[2], 10, 64)
		if e1 != nil || e2 != nil || e3 != nil || pid <= 1 || start == 0 || seen[pid] || rss > math.MaxUint64-total {
			return 0, errors.New("native descendants unavailable")
		}
		seen[pid] = true
		total += rss
	}
	return total, nil
}

func p08DeletedScratchTotals(s p08ScratchSnapshot) (uint64, uint64, error) {
	if !s.valid {
		return 0, 0, errors.New("deleted scratch unavailable")
	}
	var count, total uint64
	for _, line := range s.deleted {
		fields := strings.Fields(line)
		if len(fields) != 5 || fields[0] != "D" {
			return 0, 0, errors.New("deleted scratch unavailable")
		}
		size, err := strconv.ParseUint(fields[4], 10, 64)
		if err != nil || size > math.MaxUint64-total {
			return 0, 0, errors.New("deleted scratch unavailable")
		}
		count++
		total += size
	}
	return count, total, nil
}

type p08ResourceEvidence struct {
	ObservedAt               time.Time              `json:"observed_at"`
	API                      DockerResourceSnapshot `json:"api"`
	Backend                  DockerResourceSnapshot `json:"backend"`
	NativeDescendantRSSBytes *uint64                `json:"native_descendant_rss_bytes"`
	APIDeletedScratchFDs     *uint64                `json:"api_deleted_scratch_fds"`
	APIDeletedScratchBytes   *uint64                `json:"api_deleted_scratch_bytes"`
	Limitation               string                 `json:"limitation"`
}

func (t p08ResourceTarget) observe(ctx context.Context) (p08ResourceEvidence, error) {
	var out p08ResourceEvidence
	before, err := t.identity(ctx)
	if err != nil {
		return out, err
	}
	process := func(pod, container, role string) ([]byte, error) {
		return t.command(ctx, "exec", pod, "-c", container, "--", "sh", "-eu", "-c", p08ProcessIdentityProbe, "p08-identity", role)
	}
	apiBefore, err := process(t.APIPod, t.APIContainer, "api")
	if err != nil {
		return out, errors.New("API process identity unavailable")
	}
	backendBefore, err := process(t.BackendPod, t.BackendContainer, "backend")
	if err != nil {
		return out, errors.New("backend process identity unavailable")
	}
	probe := func(pod, container, role string) (DockerResourceSnapshot, *int, error) {
		data, err := t.command(ctx, "exec", pod, "-c", container, "--", "sh", "-eu", "-c", dockerResourceProbe, "p08-resource", role)
		if err != nil {
			return DockerResourceSnapshot{}, nil, errors.New("resource probe unavailable")
		}
		return parseResourceSnapshot(data, role)
	}
	api, _, err := probe(t.APIPod, t.APIContainer, "api")
	if err != nil || api.OwnershipLockFiles == nil {
		return out, errors.New("API resources unavailable")
	}
	backend, native, err := probe(t.BackendPod, t.BackendContainer, "backend")
	if err != nil {
		return out, errors.New("backend resources unavailable")
	}
	var backendStart, backendPID, backendNative uint64
	if parseP08ProcessIdentity(backendBefore, &backendStart, &backendPID, &backendNative) != nil || backendPID <= 1 || backendNative == 0 || native == nil || uint64(*native) != backendPID {
		return out, errors.New("native identity unavailable")
	}
	data, err := t.command(ctx, "exec", t.BackendPod, "-c", t.BackendContainer, "--", "sh", "-eu", "-c", p08NativeDescendantProbe, "p08-descendants", "/proc", strconv.FormatUint(backendPID, 10), strconv.FormatUint(backendNative, 10))
	if err != nil {
		return out, errors.New("native descendant observation unavailable")
	}
	descendantRSS, err := p08NativeDescendantRSS(data)
	if err != nil {
		return out, err
	}
	data, err = t.command(ctx, "exec", t.APIPod, "-c", t.APIContainer, "--", "sh", "-eu", "-c", p08ScratchProbe, "p08-scratch", "/tmp", "/proc", "1")
	if err != nil {
		return out, errors.New("API scratch observation unavailable")
	}
	scratch, err := parseP08Scratch(data)
	if err != nil {
		return out, errors.New("API scratch observation unavailable")
	}
	deletedFDs, deletedBytes, err := p08DeletedScratchTotals(scratch)
	if err != nil {
		return out, err
	}
	apiAfter, err := process(t.APIPod, t.APIContainer, "api")
	if err != nil {
		return out, errors.New("API process identity unavailable")
	}
	backendAfter, err := process(t.BackendPod, t.BackendContainer, "backend")
	if err != nil {
		return out, errors.New("backend process identity unavailable")
	}
	after, err := t.identity(ctx)
	// Compare the native PID returned by the actual resource probe too.
	var apiStart, apiPID, apiNative uint64
	if err != nil || before != after || !bytes.Equal(apiBefore, apiAfter) || !bytes.Equal(backendBefore, backendAfter) || parseP08ProcessIdentity(apiBefore, &apiStart, &apiPID, &apiNative) != nil || parseP08ProcessIdentity(backendBefore, &backendStart, &backendPID, &backendNative) != nil || apiPID != 0 || apiNative != 0 || backendPID <= 1 || backendNative == 0 || native == nil || uint64(*native) != backendPID {
		return out, errors.New("resource owner changed or identity unavailable")
	}
	out = p08ResourceEvidence{ObservedAt: time.Now().UTC(), API: api, Backend: backend, NativeDescendantRSSBytes: &descendantRSS, APIDeletedScratchFDs: &deletedFDs, APIDeletedScratchBytes: &deletedBytes, Limitation: "serial snapshots; native descendant RSS is separate from the P07 aggregate; API decoder RSS is excluded; no memory stability or GPU activity conclusion"}
	return out, nil
}

func parseP08ProcessIdentity(data []byte, start, pid, native *uint64) error {
	fields := strings.Fields(string(data))
	if len(fields) != 3 {
		return errors.New("process identity unavailable")
	}
	values := []*uint64{start, pid, native}
	for i, s := range fields {
		v, err := strconv.ParseUint(s, 10, 64)
		if err != nil {
			return errors.New("process identity unavailable")
		}
		*values[i] = v
	}
	if *start == 0 {
		return errors.New("process identity unavailable")
	}
	return nil
}

func readP08ResourceTarget(path string) (p08ResourceTarget, error) {
	var t p08ResourceTarget
	if !filepath.IsAbs(path) {
		return t, errors.New("absolute private selection required")
	}
	f, err := os.Open(path)
	if err != nil {
		return t, errors.New("private selection unavailable")
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 {
		return t, errors.New("private selection must be a regular mode 0600 file")
	}
	data, err := io.ReadAll(io.LimitReader(f, 65537))
	if err != nil || len(data) > 65536 {
		return t, errors.New("private selection unavailable or too large")
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if d.Decode(&t) != nil || d.Decode(new(any)) != io.EOF || !t.valid() {
		return p08ResourceTarget{}, errors.New("invalid private resource selection")
	}
	return t, nil
}

func TestP08ResourceSnapshot(t *testing.T) {
	if os.Getenv("P08_RESOURCE_SNAPSHOT") != "1" {
		t.Skip("requires an authorized parent-owned Kubernetes pair")
	}
	target, err := readP08ResourceTarget(os.Getenv("P08_RESOURCE_TARGET"))
	if err != nil {
		t.Fatal(err)
	}
	dir := os.Getenv("P08_RESOURCE_EVIDENCE_DIR")
	if !filepath.IsAbs(dir) {
		t.Fatal("absolute private evidence directory required")
	}
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() || info.Mode().Perm() != 0700 {
		t.Fatal("existing mode 0700 private evidence directory required")
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	evidence, err := target.observe(ctx)
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.MarshalIndent(evidence, "", "  ")
	if err != nil {
		t.Fatal("resource evidence encoding failed")
	}
	f, err := os.OpenFile(filepath.Join(dir, "p08-resource-snapshot.json"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal("resource evidence creation failed")
	}
	_, werr := f.Write(append(data, '\n'))
	cerr := f.Close()
	if werr != nil || cerr != nil {
		t.Fatal("resource evidence write failed")
	}
	t.Log("partial serial resource snapshot recorded; memory stability and GPU activity are not established")
}
