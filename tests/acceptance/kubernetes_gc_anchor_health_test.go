package acceptance

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// One bounded exec handles a small number of ordinary backend health GETs.
// Arguments are validated before exec; no endpoint or error output is emitted.
const p08GCBackendHealthScript = `set -eu
port=$1
count=$2
expected=$3
i=0
while [ "$i" -lt "$count" ]; do
  i=$((i + 1))
  code=$(curl --silent --noproxy '*' --connect-timeout 1 --max-time 2 --output /dev/null --write-out '%{http_code}' "http://127.0.0.1:$port/health") || { printf '%s failed\n' "$i"; exit 0; }
  [ "$code" = "$expected" ] || { printf '%s failed\n' "$i"; exit 0; }
done
printf '%s ok\n' "$i"`

const p08GCBackendHealthChunk = 16

type p08BackendHealthObservation struct {
	StartedAt, FinishedAt, Deadline time.Time
	Calls, Errors                   int
	CallsUnknown                    bool
}

func runP08BackendHealthObservation(ctx context.Context, target p08ResourceTarget, port string, facts *p08BackendHealthObservation, calls int) error {
	deadline, ok := ctx.Deadline()
	parsedPort, portErr := strconv.Atoi(port)
	if !ok || facts == nil || portErr != nil || parsedPort < 1 || parsedPort > 65535 || strconv.Itoa(parsedPort) != port || calls < 0 || calls > residentHealthObservationMaxCalls-facts.Calls || facts.CallsUnknown || (!facts.Deadline.IsZero() && !facts.Deadline.Equal(deadline)) {
		return errResidentSupervisorHeapUnknown
	}
	if facts.StartedAt.IsZero() {
		facts.StartedAt = time.Now()
	}
	facts.Deadline = deadline
	defer func() { facts.FinishedAt = time.Now() }()
	for remaining := calls; remaining > 0; {
		if ctx.Err() != nil {
			return errResidentSupervisorHeapUnknown
		}
		chunk := min(remaining, p08GCBackendHealthChunk)
		data, err := target.command(ctx, "exec", target.BackendPod, "-c", target.BackendContainer, "--", "sh", "-ec", p08GCBackendHealthScript, "p08-gc-health", port, strconv.Itoa(chunk), strconv.Itoa(http.StatusOK))
		if err != nil || ctx.Err() != nil {
			facts.CallsUnknown = true
			facts.Errors++
			return errResidentSupervisorHeapUnknown
		}
		fields := strings.Fields(string(data))
		if len(fields) != 2 {
			facts.CallsUnknown = true
			facts.Errors++
			return errResidentSupervisorHeapUnknown
		}
		n, parseErr := strconv.Atoi(fields[0])
		if parseErr != nil || n < 1 || n > chunk || strconv.Itoa(n) != fields[0] || (fields[1] != "ok" && fields[1] != "failed") || (fields[1] == "ok" && n != chunk) {
			facts.CallsUnknown = true
			facts.Errors++
			return errResidentSupervisorHeapUnknown
		}
		facts.Calls += n
		if fields[1] == "failed" {
			facts.Errors++
			return errResidentSupervisorHeapUnknown
		}
		remaining -= n
	}
	return nil
}
