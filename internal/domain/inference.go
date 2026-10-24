package domain

type BackendState string

const (
	BackendStarting   BackendState = "starting"
	BackendReady      BackendState = "ready"
	BackendBusy       BackendState = "busy"
	BackendRecovering BackendState = "recovering"
	BackendStopping   BackendState = "stopping"
	BackendFailed     BackendState = "failed"
)

type BackendCompatibility struct {
	ProtocolVersion          string
	ConfigurationFingerprint string
	ProcessGeneration        string
}

type BackendHealth struct {
	BackendCompatibility
	Status       BackendState
	Model        string
	Multilingual bool
	CUDAReady    bool
}

type InferenceFields struct {
	Language    string
	Prompt      string
	Temperature float64
}

type TranscriptionResult struct {
	Text string
}
