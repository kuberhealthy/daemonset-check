# Kuberhealthy Daemonset Check

This check validates that DaemonSets can be scheduled on all eligible nodes, and that their pods can be removed cleanly afterward. It deploys a DaemonSet, waits for pods to become ready, deletes the DaemonSet, and confirms pods are removed.

## What It Does

1. Deploys a DaemonSet using the pause container image.
2. Waits for all eligible nodes to run a daemonset pod.
3. Deletes the DaemonSet and waits for removal.
4. Reissues pod deletes until all daemonset pods are gone.

## Configuration

All configuration is controlled via environment variables.

- `POD_NAMESPACE`: Namespace to deploy the daemonset in. Default `kuberhealthy`.
- `PAUSE_CONTAINER_IMAGE`: Pause image used for the DaemonSet. Default `gcr.io/google-containers/pause:3.1`.
- `SHUTDOWN_GRACE_PERIOD`: Grace period before force shutdown. Default `1m`.
- `CHECK_DAEMONSET_NAME`: Base daemonset name. Default `daemonset`.
- `DAEMONSET_PRIORITY_CLASS_NAME`: Priority class name for daemonset pods. Default empty.
- `NODE_SELECTOR`: Comma-separated `key=value` selectors for eligible nodes.
- `TOLERATIONS`: Comma-separated tolerations in `key=value:effect` or `key:effect` format.
- `ALLOWED_TAINTS`: Comma-separated taints that should be excluded from automatic tolerations.

Kuberhealthy injects these variables automatically into the check pod:

- `KH_REPORTING_URL`
- `KH_RUN_UUID`
- `KH_CHECK_RUN_DEADLINE`

## Build

Use the `Justfile` to build or test the check:

```bash
just build
just test
```

## Example HealthCheck

```yaml
apiVersion: kuberhealthy.github.io/v2
kind: HealthCheck
metadata:
  name: daemonset
  namespace: kuberhealthy
spec:
  runInterval: 15m
  timeout: 12m
  podSpec:
    spec:
      serviceAccountName: daemonset-khcheck
      containers:
        - name: daemonset-check
          image: kuberhealthy/daemonset-check:sha-<short-sha>
          imagePullPolicy: IfNotPresent
          env:
            - name: POD_NAMESPACE
              value: "kuberhealthy"
            - name: NODE_SELECTOR
              value: "kubernetes.io/os=linux"
          resources:
            requests:
              cpu: 10m
              memory: 50Mi
      restartPolicy: Never
```

A full install bundle with RBAC is available in `healthcheck.yaml`.

## Image Tags

- `sha-<short-sha>` tags are published on every push to `main`.
- `vX.Y.Z` tags are published when a matching Git tag is pushed.
