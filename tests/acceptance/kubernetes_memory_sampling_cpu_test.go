package acceptance

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestP08MemoryDecoderSamplesDuringWorkAndFailsClosed(t *testing.T) {
	for _, tc := range []struct {
		name      string
		sampleErr bool
		probeErr  bool
		nilSample bool
		workErr   bool
		wantErr   bool
	}{
		{name: "sample during work"}, {name: "unknown", nilSample: true, wantErr: true}, {name: "private failure", sampleErr: true, wantErr: true}, {name: "probe failure", probeErr: true, wantErr: true}, {name: "work failure", workErr: true, wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			started := make(chan struct{})
			sampled := make(chan struct{})
			var sampledOnce sync.Once
			work := func(ctx context.Context) (p08RepeatedResources, error) {
				close(started)
				select {
				case <-sampled:
				case <-ctx.Done():
					return p08RepeatedResources{}, ctx.Err()
				}
				if tc.workErr {
					return p08RepeatedResources{}, errors.New("work")
				}
				return p08RepeatedResources{Requests: 30}, nil
			}
			observe := func(ctx context.Context) (*uint64, error) {
				select {
				case <-started:
				case <-ctx.Done():
					return nil, ctx.Err()
				}
				sampledOnce.Do(func() { close(sampled) })
				if tc.sampleErr {
					return nil, errors.New("private")
				}
				if tc.probeErr {
					return nil, p08DecoderProbe
				}
				if tc.nilSample {
					return nil, nil
				}
				n := uint64(4096)
				return &n, nil
			}
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()
			result, decoder, err := p08MeasureDecoderDuringWork(ctx, observe, work)
			if (err != nil) != tc.wantErr {
				t.Fatalf("unexpected result: %v", err)
			}
			if tc.probeErr && (err == nil || err.Error() != "decoder observation: probe" || decoder.FailureCategory != "probe" || decoder.FailedSamples != 1 || decoder.Complete) {
				t.Fatal("safe probe failure category or incomplete count was lost")
			}
			if (tc.nilSample || tc.sampleErr) && (decoder.FailureCategory != "unknown" || decoder.FailedSamples != 1 || decoder.Complete) {
				t.Fatal("unknown failure was not recorded safely")
			}
			if !tc.wantErr && (result.Requests != 30 || !decoder.Complete || decoder.Samples != 1 || decoder.PeakRSSBytes == nil || *decoder.PeakRSSBytes != 4096) {
				t.Fatal("work and measured sample not retained")
			}
			if tc.nilSample && decoder.PeakRSSBytes != nil {
				t.Fatal("unknown sample became measured zero")
			}
		})
	}
}

func TestP08MemoryDecoderFailurePreservesPartialEvidence(t *testing.T) {
	calls := 0
	work := func(ctx context.Context) (p08RepeatedResources, error) {
		<-ctx.Done()
		return p08RepeatedResources{Requests: 1}, ctx.Err()
	}
	observe := func(context.Context) (*uint64, error) {
		calls++
		if calls == 1 {
			value := uint64(4096)
			return &value, nil
		}
		return nil, p08DecoderOwnerAfter
	}
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	result, sampling, err := p08MeasureDecoderDuringWork(ctx, observe, work)
	if err == nil || result.Requests != 1 || sampling.Complete || sampling.Samples != 1 || sampling.FailedSamples != 1 || sampling.FailureCategory != "owner_after" || sampling.PeakRSSBytes == nil || *sampling.PeakRSSBytes != 4096 {
		t.Fatal("failed later observation lost the partial sample or became complete")
	}
}
