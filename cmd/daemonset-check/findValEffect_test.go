package main

import "testing"

// TestFindValEffect verifies value/effect parsing.
func TestFindValEffect(t *testing.T) {
	// Build the input string.
	stringTol := "test:test"

	// Define expected output.
	expectedResults := []string{"test", "test"}

	// Parse the string.
	value, effect, err := findValEffect(stringTol)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Validate parsed value and effect.
	if value != expectedResults[0] {
		t.Fatalf("expected %s got %s", expectedResults[0], value)
	}
	if effect != expectedResults[1] {
		t.Fatalf("expected %s got %s", expectedResults[1], effect)
	}
}
