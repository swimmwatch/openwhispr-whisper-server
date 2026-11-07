package acceptance

import (
	"context"
	"errors"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"
)

var errP08ConditioningUnknown = errors.New("Kubernetes conditioning resource evidence unavailable")

type p08MemoryObservationStage string

const (
	p08MemorySelection    p08MemoryObservationStage = "selection"
	p08MemoryOwnerBefore  p08MemoryObservationStage = "owner_before"
	p08MemoryGPUIdentity  p08MemoryObservationStage = "gpu_identity"
	p08MemoryPIDMapping   p08MemoryObservationStage = "pid_mapping"
	p08MemoryResource     p08MemoryObservationStage = "resource"
	p08MemoryBreakdown    p08MemoryObservationStage = "breakdown"
	p08MemoryDecoder      p08MemoryObservationStage = "decoder"
	p08MemoryGPUQuery     p08MemoryObservationStage = "gpu_allocation_query"
	p08MemoryGPUParse     p08MemoryObservationStage = "gpu_allocation_parse"
	p08MemoryGPUMissing   p08MemoryObservationStage = "gpu_allocation_missing"
	p08MemoryGPUAmbiguous p08MemoryObservationStage = "gpu_allocation_ambiguous"
	p08MemoryGPUOverflow  p08MemoryObservationStage = "gpu_allocation_overflow"
	p08MemoryGPUAfter     p08MemoryObservationStage = "gpu_identity_after"
	p08MemoryOwnerAfter   p08MemoryObservationStage = "owner_after"
	p08MemoryPoint        p08MemoryObservationStage = "point"
)

func (s p08MemoryObservationStage) Error() string { return "memory observation: " + string(s) }

type p08MemoryIdentityFailure struct {
	Stage  p08MemoryObservationStage
	Detail p08CUDAIdentityStage
}

func (e p08MemoryIdentityFailure) Error() string { return e.Stage.Error() }

func p08MemoryIdentityError(stage p08MemoryObservationStage, err error) error {
	var detail p08CUDAIdentityStage
	if err == nil {
		detail = p08CUDAIdentityMismatch
	} else if !errors.As(err, &detail) {
		detail = ""
	}
	return p08MemoryIdentityFailure{Stage: stage, Detail: detail}
}

// Descendant observations remain separate from the unchanged P07 aggregate.
type p08ConditioningObservation struct {
	residentResourcePoint
	NativeDescendantRSSBytes *uint64
}

type p08ConditioningEpoch struct {
	residentConditioningEpoch
	NativeDescendantRSSBytes []uint64
	RequestsAttempted        *int
	ObservationPhase         string
	ObserverStage            string
	ObserverDetail           string
}

// No unknown input is converted to zero.
func p08MemoryConditioningPoint(e p08ResourceEvidence, breakdown *residentRSSBreakdown, decoder, gpu *uint64) (p08ConditioningObservation, error) {
	if e.ObservedAt.IsZero() || e.API.RSSBytes == nil || e.API.FDCount == nil || e.API.OwnershipLockFiles == nil || e.Backend.RSSBytes == nil || e.NativeDescendantRSSBytes == nil || e.APIDeletedScratchFDs == nil || e.APIDeletedScratchBytes == nil || decoder == nil || gpu == nil || *gpu == 0 || breakdown == nil || breakdown.At.IsZero() {
		return p08ConditioningObservation{}, errP08ConditioningUnknown
	}
	if releasedScratchResources(e.API) != nil || *e.APIDeletedScratchFDs != 0 || *e.APIDeletedScratchBytes != 0 || *decoder != 0 {
		return p08ConditioningObservation{}, errors.New("completed API resources retained or unavailable")
	}
	for _, p := range []residentProcessRSS{breakdown.Supervisor, breakdown.Native} {
		if p.RSSBytes == 0 || p.AnonymousBytes > p.RSSBytes {
			return p08ConditioningObservation{}, errP08ConditioningUnknown
		}
	}
	return p08ConditioningObservation{residentResourcePoint: residentResourcePoint{At: e.ObservedAt, APIRSSBytes: *e.API.RSSBytes, BackendRSSBytes: *e.Backend.RSSBytes, GPUBytes: *gpu, APIFDs: *e.API.FDCount, APIDeletedScratchFDs: *e.APIDeletedScratchFDs, APIDeletedScratchBytes: *e.APIDeletedScratchBytes, BackendProcessRSS: breakdown}, NativeDescendantRSSBytes: e.NativeDescendantRSSBytes}, nil
}

// The host PID must actually identify the same namespace PID and kernel start
// time. Similar names, a health generation or a GPU listing alone are not a map.
func p08MemoryNamespacePID(status []byte, hostPID int, namespacePID uint64) error {
	found := false
	for _, line := range strings.Split(string(status), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 || fields[0] != "NSpid:" {
			continue
		}
		if found || len(fields) < 3 {
			return errP08ConditioningUnknown
		}
		found = true
		for i, v := range fields[1:] {
			n, err := strconv.ParseUint(v, 10, 64)
			if err != nil || n == 0 || (i == 0 && n != uint64(hostPID)) || (i == len(fields)-2 && n != namespacePID) {
				return errP08ConditioningUnknown
			}
		}
	}
	if !found {
		return errP08ConditioningUnknown
	}
	return nil
}

// One actual P07 conditioning block; the caller must pass this to the shared
// runResidentConditioning policy, then obtain independent GC proof before freeze.
// This helper cannot establish an envelope or qualify a run by itself.
func p08MemoryConditioningEpoch(ctx context.Context, observer p08MemoryConditioningObserver, client *PublicClient, audio []byte) (p08ConditioningEpoch, error) {
	var epoch p08ConditioningEpoch
	if _, ok := ctx.Deadline(); !ok || client == nil || len(audio) == 0 || observer.model == "" {
		return epoch, errP08ConditioningUnknown
	}
	observe := func(phase string) (p08ConditioningObservation, error) {
		point, err := observer.observe(ctx)
		if err != nil {
			epoch.ObservationPhase = phase
			epoch.ObserverStage = "unknown"
			var stage p08MemoryObservationStage
			var identityFailure p08MemoryIdentityFailure
			if errors.As(err, &identityFailure) {
				stage = identityFailure.Stage
				switch identityFailure.Detail {
				case p08CUDAIdentityNamespace, p08CUDAIdentityDriver, p08CUDAIdentityPod, p08CUDAIdentityGPU, p08CUDAIdentityEnumeration, p08CUDAIdentityEnumGuard, p08CUDAIdentityEnumAccess, p08CUDAIdentityEnumIO, p08CUDAIdentityEnumFormat, p08CUDAIdentityEnumUnconfirmed, p08CUDAIdentityCgroup, p08CUDAIdentityStat, p08CUDAIdentityHealth, p08CUDAIdentityMissing, p08CUDAIdentityAmbiguous, p08CUDAIdentityMismatch:
					epoch.ObserverDetail = string(identityFailure.Detail)
				default:
					epoch.ObserverDetail = "unknown"
				}
			} else {
				errors.As(err, &stage)
			}
			if stage != "" {
				switch stage {
				case p08MemorySelection, p08MemoryOwnerBefore, p08MemoryGPUIdentity, p08MemoryPIDMapping, p08MemoryResource, p08MemoryBreakdown, p08MemoryDecoder, p08MemoryGPUQuery, p08MemoryGPUParse, p08MemoryGPUMissing, p08MemoryGPUAmbiguous, p08MemoryGPUOverflow, p08MemoryGPUAfter, p08MemoryOwnerAfter, p08MemoryPoint:
					epoch.ObserverStage = string(stage)
				}
			}
		}
		return point, err
	}
	if _, err := observe("preflight"); err != nil {
		return epoch, err
	}
	attempted := 0
	epoch.RequestsAttempted = &attempted
	for i := 0; i < residentConditioningBlockRequests; i++ {
		attempted++
		reply, err := client.Transcribe(ctx, audio, "jfk.wav", []FormField{{"model", observer.model}, {"language", "en"}, {"temperature", "0"}})
		if err != nil || reply.Status != http.StatusOK {
			return epoch, errors.New("conditioning public request failed without retry")
		}
	}
	completed, err := observe("completion")
	if err != nil {
		return epoch, err
	}
	epoch.Completion = completed.residentResourcePoint
	epoch.NativeDescendantRSSBytes = append(epoch.NativeDescendantRSSBytes, *completed.NativeDescendantRSSBytes)
	for i := 0; i < 5; i++ {
		point, err := observe("idle")
		if err != nil {
			return epoch, err
		}
		epoch.Idle = append(epoch.Idle, point.residentResourcePoint)
		epoch.NativeDescendantRSSBytes = append(epoch.NativeDescendantRSSBytes, *point.NativeDescendantRSSBytes)
		if i < 4 {
			timer := time.NewTimer(time.Second)
			select {
			case <-ctx.Done():
				timer.Stop()
				return epoch, ctx.Err()
			case <-timer.C:
			}
		}
	}
	return epoch, nil
}

type p08MemoryConditioningObserver struct {
	target      p08ResourceTarget
	gpu         kubernetesCUDAActivity
	initial     p08RepeatedIdentity
	model, port string
}

// observe performs only completed-operation reads; serial sources are separately
// fenced by the exact target and lifetime. It is not a continuous peak sampler.
func (o p08MemoryConditioningObserver) observe(ctx context.Context) (p08ConditioningObservation, error) {
	target, device := o.target, o.gpu.observer
	if device.Kubeconfig != target.Kubeconfig || device.Context != target.Context || device.Namespace != target.Namespace || device.NamespaceUID != target.NamespaceUID || device.Release != target.Release || device.OldPodUID != target.BackendUID || o.gpu.backendPod != target.BackendPod || strconv.Itoa(o.gpu.backendPort) != o.port {
		return p08ConditioningObservation{}, p08MemorySelection
	}
	owner := func(ctx context.Context) (p08RepeatedIdentity, error) {
		id, err := p08RepeatedOwner(ctx, target, o.model, o.port)
		if err != nil || id != o.initial {
			return p08RepeatedIdentity{}, errP08ConditioningUnknown
		}
		return id, nil
	}
	if _, err := owner(ctx); err != nil {
		return p08ConditioningObservation{}, p08MemoryOwnerBefore
	}
	gpuBefore, err := o.gpu.identity(ctx)
	if err != nil || gpuBefore.generation != o.initial.generation || gpuBefore.start != strconv.FormatUint(o.initial.nativeStart, 10) {
		return p08ConditioningObservation{}, p08MemoryIdentityError(p08MemoryGPUIdentity, err)
	}
	status, err := device.command(ctx, device.DriverNamespace, "exec", device.DriverPod, "--", "cat", "/proc/"+strconv.Itoa(gpuBefore.pid)+"/status")
	if err != nil || p08MemoryNamespacePID(status, gpuBefore.pid, o.initial.nativePID) != nil {
		return p08ConditioningObservation{}, p08MemoryPIDMapping
	}
	evidence, err := target.observe(ctx)
	if err != nil {
		return p08ConditioningObservation{}, p08MemoryResource
	}
	at := time.Now().UTC()
	data, err := target.command(ctx, "exec", target.BackendPod, "-c", target.BackendContainer, "--", "sh", "-eu", "-c", residentProcessRSSProbe, "p08-conditioning-rss", "1", strconv.FormatUint(o.initial.nativePID, 10))
	if err != nil {
		return p08ConditioningObservation{}, p08MemoryBreakdown
	}
	breakdown, err := parseResidentRSSBreakdown(data, at)
	if err != nil {
		return p08ConditioningObservation{}, p08MemoryBreakdown
	}
	decoder, err := p08MemoryDecoderRSS(ctx, target, o.initial, owner)
	if err != nil {
		return p08ConditioningObservation{}, p08MemoryDecoder
	}
	mib, err := o.gpu.allocation(ctx, gpuBefore.pid)
	if err != nil {
		var allocationError p08GPUAllocationError
		if errors.As(err, &allocationError) {
			switch allocationError {
			case p08GPUAllocationQuery:
				return p08ConditioningObservation{}, p08MemoryGPUQuery
			case p08GPUAllocationParse:
				return p08ConditioningObservation{}, p08MemoryGPUParse
			case p08GPUAllocationMissing:
				return p08ConditioningObservation{}, p08MemoryGPUMissing
			case p08GPUAllocationAmbiguous:
				return p08ConditioningObservation{}, p08MemoryGPUAmbiguous
			}
		}
		return p08ConditioningObservation{}, p08MemoryGPUQuery
	}
	if mib == 0 || mib > math.MaxUint64/(1<<20) {
		return p08ConditioningObservation{}, p08MemoryGPUOverflow
	}
	gpuBytes := mib * (1 << 20)
	gpuAfter, err := o.gpu.identity(ctx)
	if err != nil || gpuAfter != gpuBefore {
		return p08ConditioningObservation{}, p08MemoryIdentityError(p08MemoryGPUAfter, err)
	}
	if _, err = owner(ctx); err != nil {
		return p08ConditioningObservation{}, p08MemoryOwnerAfter
	}
	point, err := p08MemoryConditioningPoint(evidence, breakdown, decoder, &gpuBytes)
	if err != nil {
		return p08ConditioningObservation{}, p08MemoryPoint
	}
	return point, nil
}
