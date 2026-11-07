package acceptance

import (
	"context"
	"errors"
	"time"
)

type p08DecoderSampling struct {
	Samples         int     `json:"samples"`
	FailedSamples   int     `json:"failed_samples"`
	FailureCategory string  `json:"failure_category,omitempty"`
	PeakRSSBytes    *uint64 `json:"peak_sampled_rss_bytes"`
	Complete        bool    `json:"complete"`
	Limitation      string  `json:"limitation"`
}

// Samples all API descendants while the existing public workload executes. The
// peak is only the maximum of serial samples, not a continuous peak or a memory
// qualification; failures and cancellation cannot turn unknown into zero.
func p08MeasureDecoderDuringWork(ctx context.Context, observe func(context.Context) (*uint64, error), work func(context.Context) (p08RepeatedResources, error)) (p08RepeatedResources, p08DecoderSampling, error) {
	var result p08RepeatedResources
	out := p08DecoderSampling{Limitation: "sampled API PID1-descendant RSS upper bound includes other descendants and concurrent observation processes; serial samples can miss decoder peaks; not calibrated memory qualification"}
	if observe == nil || work == nil {
		return result, out, errors.New("decoder sampling unavailable")
	}
	child, cancel := context.WithCancel(ctx)
	defer cancel()
	type workResult struct {
		value p08RepeatedResources
		err   error
	}
	done := make(chan workResult, 1)
	go func() { v, err := work(child); done <- workResult{v, err} }()
	for {
		value, err := observe(child)
		if err != nil || value == nil {
			out.FailedSamples++
			out.FailureCategory = "unknown"
			var category p08DecoderError
			if errors.As(err, &category) {
				switch category {
				case p08DecoderIdentity, p08DecoderOwnerBefore, p08DecoderProbe, p08DecoderParse, p08DecoderOwnerAfter:
					out.FailureCategory = string(category)
				}
			}
			cancel()
			completed := <-done
			if out.FailureCategory != "unknown" {
				return completed.value, out, category
			}
			return completed.value, out, errors.New("decoder sampling unavailable")
		}
		out.Samples++
		if out.PeakRSSBytes == nil || *value > *out.PeakRSSBytes {
			n := *value
			out.PeakRSSBytes = &n
		}
		timer := time.NewTimer(250 * time.Millisecond)
		select {
		case completed := <-done:
			timer.Stop()
			out.Complete = completed.err == nil && child.Err() == nil
			if !out.Complete && completed.err == nil {
				return completed.value, out, errors.New("decoder sampling cancelled")
			}
			return completed.value, out, completed.err
		case <-child.Done():
			timer.Stop()
			completed := <-done
			return completed.value, out, errors.New("decoder sampling cancelled")
		case <-timer.C:
		}
	}
}
