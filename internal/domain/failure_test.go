package domain

import (
	"errors"
	"fmt"
	"testing"
)

func TestFailureWrappingAndTransportIndependence(t *testing.T) {
	cause := errors.New("private cause")
	original := &Failure{
		Kind:  InvalidInput,
		Field: "model",
		Cause: cause,
	}

	wrapped := fmt.Errorf("application: %w", original)

	var failure *Failure
	if !errors.As(wrapped, &failure) ||
		failure.Kind != InvalidInput ||
		failure.Field != "model" ||
		!errors.Is(wrapped, cause) {
		t.Fatal("wrapped failure lost classification or cause")
	}
	if original.Error() == "" {
		t.Fatal("missing error description")
	}

}
