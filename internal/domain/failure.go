package domain

type FailureKind string

const (
	InvalidInput       FailureKind = "invalid_input"
	UnsupportedAudio   FailureKind = "unsupported_audio"
	AudioTooLong       FailureKind = "audio_too_long"
	UnknownModel       FailureKind = "unknown_model"
	QueueFull          FailureKind = "queue_full"
	DeadlineExceeded   FailureKind = "deadline_exceeded"
	NotReady           FailureKind = "not_ready"
	BackendUnavailable FailureKind = "backend_unavailable"
	GPUUnavailable     FailureKind = "gpu_unavailable"
	ProvisioningFailed FailureKind = "provisioning_failed"
	ShuttingDown       FailureKind = "shutting_down"
	Internal           FailureKind = "internal"
)

// Failure classifies service errors without carrying transport presentation.
type Failure struct {
	Kind  FailureKind
	Field string
	Cause error
}

func (f *Failure) Error() string {
	return "service failure: " + string(f.Kind)
}

func (f *Failure) Unwrap() error {
	return f.Cause
}
