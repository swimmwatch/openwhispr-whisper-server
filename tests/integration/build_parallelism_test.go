package integration

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestBuildParallelism(t *testing.T) {
	imageTests(t)

	status, err := os.ReadFile("/proc/self/status")
	if err != nil {
		t.Fatal(err)
	}

	allowed := regexp.MustCompile(`(?m)^Cpus_allowed_list:\s*(\d+)`).FindSubmatch(status)
	if len(allowed) != 2 {
		t.Fatal("cannot identify an available CPU for the affinity case")
	}

	cases := []struct {
		name      string
		resources []string
		override  string
		maxJobs   int
		exact     bool
	}{
		{
			name:      "one_cpu",
			resources: []string{"cpu-quota=100000", "cpu-period=100000", "memory=8g"},
			override:  "",
			maxJobs:   1,
			exact:     true,
		},
		{
			name:      "fractional_cpu",
			resources: []string{"cpu-quota=150000", "cpu-period=100000", "memory=8g"},
			override:  "",
			maxJobs:   2,
			exact:     false,
		},
		{
			name:      "affinity_without_quota",
			resources: []string{"cpuset-cpus=" + string(allowed[1]), "memory=8g"},
			override:  "",
			maxJobs:   1,
			exact:     true,
		},
		{
			name:      "memory_cap",
			resources: []string{"cpu-quota=800000", "cpu-period=100000", "memory=256m"},
			override:  "",
			maxJobs:   1,
			exact:     true,
		},
		{
			name:      "explicit_override",
			resources: []string{"cpu-quota=100000", "cpu-period=100000", "memory=256m"},
			override:  "3",
			maxJobs:   3,
			exact:     true,
		},
	}
	for _, role := range []string{"api", "backend"} {
		t.Run(role, func(t *testing.T) {
			source := string(artifact(t, "containers/"+role+".Dockerfile"))
			bases := defaultBaseImages([]byte(source))
			if len(bases) != 3 {
				t.Fatal("missing native compiler base")
			}
			// Execute the real job selection without downloading or compiling sources.
			var budgets strings.Builder
			for _, name := range []string{"BUILD_MEMORY_RESERVE_KIB", "BUILD_MEMORY_PER_JOB_KIB"} {
				declaration := regexp.MustCompile(`(?m)^ARG ` + name + `=[0-9]+$`).FindString(source)
				if declaration == "" {
					t.Fatalf("missing build memory budget %s", name)
				}
				budgets.WriteString(declaration + "\n")
			}

			definition := fmt.Sprintf(`FROM %s
SHELL ["/bin/bash","-euo","pipefail","-c"]
ARG BUILD_JOBS
%sCOPY --chmod=0555 containers/scripts/build-jobs.sh /usr/local/bin/whisper-build-jobs
RUN <<BUILD
jobs=$(/usr/local/bin/whisper-build-jobs)
printf 'BUILD_JOBS=%%s\n' "$jobs"
BUILD
`, bases[1], budgets.String())
			for _, tc := range cases {
				t.Run(tc.name, func(t *testing.T) {
					ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
					defer cancel()

					args := []string{
						"buildx", "build", "--platform=linux/amd64", "--no-cache", "--network=none",
						"--output=type=cacheonly", "--progress=plain", "-f", "-",
					}
					for _, resource := range tc.resources {
						args = append(args, "--resource", resource)
					}
					if tc.override != "" {
						args = append(args, "--build-arg", "BUILD_JOBS="+tc.override)
					}
					args = append(args, ".")
					cmd := exec.CommandContext(ctx, "docker", args...)
					cmd.Dir = root(t)
					cmd.Stdin = bytes.NewBufferString(definition)
					output, err := cmd.CombinedOutput()
					if err != nil {
						t.Fatalf("resource-constrained BuildKit execution: %v\n%s", err, output)
					}

					selected := regexp.MustCompile(`(?m)^#\d+\s+[0-9.]+ BUILD_JOBS=(\d+)\s*$`).FindSubmatch(output)
					if len(selected) != 2 {
						t.Fatalf("missing selected job count:\n%s", output)
					}

					jobs, err := strconv.Atoi(string(selected[1]))
					if err != nil || jobs < 1 || jobs > tc.maxJobs || tc.exact && jobs != tc.maxJobs {
						t.Fatalf("selected BUILD_JOBS=%s; expected 1..%d (exact=%t)", selected[1], tc.maxJobs, tc.exact)
					}
					t.Logf("selected BUILD_JOBS=%d", jobs)
				})
			}
		})
	}
}
