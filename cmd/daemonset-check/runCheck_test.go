package main

import "testing"

// TestNodeLabelsMatch verifies node label matching against selectors.
func TestNodeLabelsMatch(t *testing.T) {
	// Define node labels and selectors.
	labels := map[string]string{
		"blah":                   "blerp",
		"kubernetes.io/hostname": "ip-10-112-79-36.us-west-2.compute.internal",
		"kubernetes.io/role":     "node",
	}
	nodeSelectors := map[string]string{
		"blah":               "blerp",
		"kubernetes.io/role": "node",
	}

	// Execute the match.
	matched := nodeLabelsMatch(labels, nodeSelectors)

	// Validate match result.
	if matched == false {
		t.Fatalf("expected true but got: %v", matched)
	}
}
