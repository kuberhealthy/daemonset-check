package main

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
)

// TestCreateToleration verifies toleration parsing for a key-only string.
func TestCreateToleration(t *testing.T) {
	// Build the toleration string.
	stringTol := "test"

	// Define expected output.
	expectedResults := corev1.Toleration{
		Key:      "test",
		Operator: corev1.TolerationOpExists,
	}

	// Parse the toleration.
	r, err := createToleration(stringTol)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Validate the result.
	if *r != expectedResults {
		t.Fatalf("expected %+v got %+v", expectedResults, r)
	}
}
