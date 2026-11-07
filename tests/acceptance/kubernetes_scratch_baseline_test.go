package acceptance

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"
)

type p08ScratchSnapshot struct {
	files, deleted []string
	valid          bool
}

// Only hashes and numeric identities leave the probe. Missing or changing owned
// process entries make the observation unknown; snapshots are serial, not continuous.
const p08ScratchProbe = `set -eu
root=$1; proc=$2; owner=$3
[ -d "$root" ] && [ ! -L "$root" ] && [ -d "$proc/$owner/fd" ] || exit 1
# Do not accept -xdev results when a nested mount could hide scratch files.
no_nested_mounts() {
 [ -r "$proc/self/mountinfo" ] || return 1
 awk -v r="$root/" 'index($5,r)==1 { found=1 } END { exit found ? 1 : 0 }' "$proc/self/mountinfo"
}
no_nested_mounts || exit 1
# Enumerate only the selected owner's tree. Every thread may own children.
# Unrelated host process churn must not invalidate this selected observation.
processes() {
 pending="$owner:0"; seen=' '; count=0; discovered=1
 while [ -n "$pending" ]; do
  next=
  for item in $pending; do
   pid=${item%%:*}; expected=${item#*:}
   case "$pid" in ''|*[!0-9]*) exit 1;; esac
   [ "$pid" -gt 0 ] || exit 1
   # This shell and all of its descendants are probe-owned, not service work.
   [ "$pid" != "$$" ] || continue
   case "$seen" in *" $pid "*) exit 1;; esac
   seen="$seen$pid "; count=$((count+1)); [ "$count" -le 4096 ] || exit 1
   IFS= read -r line < "$proc/$pid/stat" || exit 1
   [ "${line%% *}" = "$pid" ] || exit 1
   rest=${line##*) }; [ "$rest" != "$line" ] || exit 1
   set -- $rest; [ "$#" -ge 20 ] || exit 1
   parent=$2; shift 19; start=$1
   case "$parent:$start" in *[!0-9:]*|'') exit 1;; esac
   [ "$start" -gt 0 ] || exit 1
   [ "$pid" = "$owner" ] || [ "$parent" = "$expected" ] || exit 1
   printf '%s:%s\n' "$pid" "$start"
   tasks=$(printf '%s\n' "$proc/$pid"/task/[0-9]*/children); threads=0
   for task in "$proc/$pid"/task/[0-9]*/children; do
    tid=${task%/children}; tid=${tid##*/}
    case "$tid" in ''|*[!0-9]*) exit 1;; esac
    threads=$((threads+1)); [ "$threads" -le 4096 ] || exit 1
    children=$(head -c 65537 "$task" && printf '.') || exit 1
    children=${children%.}; [ "${#children}" -le 65536 ] || exit 1
    for child in $children; do
     case "$child" in ''|*[!0-9]*) exit 1;; esac
     [ "$child" != "$$" ] || continue
     discovered=$((discovered+1)); [ "$discovered" -le 4096 ] || exit 1
     next="$next $child:$pid"
    done
   done
   [ "$tasks" = "$(printf '%s\n' "$proc/$pid"/task/[0-9]*/children)" ] || exit 1
  done
  pending=$next
 done
}
selected=$(processes); [ -n "$selected" ] || exit 1
files=$(find "$root" -xdev -type f -exec sh -eu -c '
 for p do
  pathhash=$(printf %s "$p" | sha256sum); pathhash=${pathhash%% *}
  meta=$(stat -c "%d:%i:%s:%y:%z" -- "$p") || exit 1
  digest=$(sha256sum < "$p"); digest=${digest%% *}
  after=$(stat -c "%d:%i:%s:%y:%z" -- "$p") || exit 1
  [ "$meta" = "$after" ] || exit 1
  metahash=$(printf %s "$meta" | sha256sum); metahash=${metahash%% *}
  printf "F %s %s %s\n" "$pathhash" "$metahash" "$digest"
 done
' sh {} +) || exit 1
printf '%s\n' "$files"
for row in $selected; do
 pid=${row%%:*}
 [ -d "$proc/$pid/fd" ] || exit 1
 for fd in "$proc/$pid/fd/"*; do
  [ -L "$fd" ] || exit 1
  target=$(readlink "$fd") || exit 1
  case "$target" in "$root"/*' (deleted)')
   h=$(printf %s "$target" | sha256sum); h=${h%% *}
   meta=$(stat -Lc '%d %i %s' -- "$fd") || exit 1
   printf 'D %s %s\n' "$h" "$meta";;
  esac
 done
done
selected_after=$(processes)
[ "$selected" = "$selected_after" ] || exit 1
no_nested_mounts || exit 1
printf 'OK\n'
`

func parseP08Scratch(data []byte) (p08ScratchSnapshot, error) {
	var s p08ScratchSnapshot
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) == 0 || lines[len(lines)-1] != "OK" {
		return s, errors.New("scratch observation incomplete")
	}
	for _, line := range lines[:len(lines)-1] {
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) == 4 && fields[0] == "F" {
			s.files = append(s.files, line)
		} else if len(fields) == 5 && fields[0] == "D" {
			s.deleted = append(s.deleted, line)
		} else {
			return p08ScratchSnapshot{}, errors.New("scratch observation unavailable")
		}
	}
	slices.Sort(s.files)
	slices.Sort(s.deleted)
	s.valid = true
	return s, nil
}

func p08ScratchReleased(before, after p08ScratchSnapshot) error {
	if !before.valid || !after.valid || len(before.deleted) != 0 || len(after.deleted) != 0 || !slices.Equal(before.files, after.files) {
		return errors.New("scratch identity changed or deleted-open scratch retained")
	}
	return nil
}

func waitP08ScratchRelease(ctx context.Context, observe func(context.Context) error) error {
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if observe(ctx) == nil && ctx.Err() == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
}

func p08ScratchLocalSnapshot(ctx context.Context, root, proc string) (p08ScratchSnapshot, error) {
	data, err := runKubernetesCommand(ctx, "sh", 10*time.Second, 1<<20, nil, "-eu", "-c", p08ScratchProbe, "p08-scratch", root, proc, strconv.Itoa(os.Getpid()))
	if err != nil {
		return p08ScratchSnapshot{}, fmt.Errorf("scratch observation unavailable: %w", err)
	}
	return parseP08Scratch(data)
}
