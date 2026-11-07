package domain

// ModelArtifact identifies a verified model ready for backend startup.
type ModelArtifact struct {
	ID       string
	Path     string
	SHA256   string
	Metadata ModelMetadata
}

// ModelMetadata describes the model capabilities used by the serving contract.
type ModelMetadata struct {
	VocabularySize int32
	Multilingual   bool
}
