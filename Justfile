IMAGE := "kuberhealthy/daemonset-check"
TAG := "latest"

# Build the daemonset check container locally.
build:
	podman build -f Containerfile -t {{IMAGE}}:{{TAG}} .

# Run the unit tests for the daemonset check.
test:
	go test ./...

# Build the daemonset check binary locally.
binary:
	go build -o bin/daemonset-check ./cmd/daemonset-check
