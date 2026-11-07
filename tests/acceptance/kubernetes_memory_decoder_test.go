package acceptance

import (
	"context"
	"strconv"
)

type p08DecoderError string

const (
	p08DecoderIdentity    p08DecoderError = "identity"
	p08DecoderOwnerBefore p08DecoderError = "owner_before"
	p08DecoderProbe       p08DecoderError = "probe"
	p08DecoderParse       p08DecoderError = "parse"
	p08DecoderOwnerAfter  p08DecoderError = "owner_after"
)

func (e p08DecoderError) Error() string { return "decoder observation: " + string(e) }

// p08MemoryDecoderRSS measures all API PID1 descendants, not just processes
// identified by name. It is a serial upper bound for decoder RSS at this instant;
// it cannot prove a peak between samples. The probe excludes its own shell.
// Private identities remain in memory and bracket the complete observation.
func p08MemoryDecoderRSS(ctx context.Context, target p08ResourceTarget, initial p08RepeatedIdentity, owner func(context.Context) (p08RepeatedIdentity, error)) (*uint64, error) {
	if owner == nil || initial.containers[0].ID == "" || initial.containers[1].ID == "" || initial.apiStart == 0 || initial.backendStart == 0 || initial.nativePID <= 1 || initial.nativeStart == 0 || initial.generation == "" || initial.fingerprint == "" {
		return nil, p08DecoderIdentity
	}
	before, err := owner(ctx)
	if err != nil || before != initial {
		return nil, p08DecoderOwnerBefore
	}
	data, err := target.command(ctx, "exec", target.APIPod, "-c", target.APIContainer, "--", "sh", "-eu", "-c", p08NativeDescendantProbe, "p08-memory-decoder", "/proc", "1", strconv.FormatUint(initial.apiStart, 10))
	if err != nil {
		return nil, p08DecoderProbe
	}
	rss, err := p08NativeDescendantRSS(data)
	if err != nil {
		return nil, p08DecoderParse
	}
	after, err := owner(ctx)
	if err != nil || after != initial {
		return nil, p08DecoderOwnerAfter
	}
	return &rss, nil
}
