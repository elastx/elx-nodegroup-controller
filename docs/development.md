# Development Guide

## Prerequisites

| Tool | Minimum Version | Purpose |
|------|----------------|---------|
| Go | 1.22 | Build the controller |
| Docker | any recent | Build container images |
| `kubectl` | any recent | Deploy to a cluster |
| `kustomize` | v4+ | Manage manifests |
| `controller-gen` | v0.14.0 | Generate CRD manifests and DeepCopy |
| `envtest` | bundled via `setup-envtest` | Run integration tests |

Install Go toolchain dependencies:

```bash
make controller-gen      # Install controller-gen locally
make envtest             # Install envtest binaries locally
```

---

## Repository Layout

```
.
├── api/
│   ├── v1alpha1/           # Deprecated CRD version (supported for migration)
│   │   └── nodegroup_types.go
│   └── v1alpha2/           # Current storage version
│       ├── groupversion_info.go
│       ├── nodegroup_types.go
│       └── zz_generated.deepcopy.go
├── config/
│   ├── crd/                # Generated CRD manifest
│   ├── default/            # Kustomization for full deployment
│   ├── manager/            # Deployment manifest
│   ├── rbac/               # RBAC manifests
│   └── samples/            # Example NodeGroup resources
├── controllers/
│   ├── nodegroup_controller.go      # Reconciliation logic
│   └── nodegroup_controller_test.go # Integration tests
├── hack/                   # Helper scripts (boilerplate header)
├── main.go                 # Controller manager entry point
├── Dockerfile
├── Makefile
└── go.mod
```

---

## Common Tasks

### Build the binary

```bash
make build
```

Output: `./bin/manager`

### Run tests

```bash
make test
```

Tests use `envtest` to spin up a local Kubernetes API server. Test coverage output is written to `cover.out`.

### Generate code

After modifying the API types in `api/v1alpha2/nodegroup_types.go`, regenerate:

```bash
# Regenerate CRD manifests (config/crd/bases/)
make manifests

# Regenerate DeepCopy methods (zz_generated.deepcopy.go)
make generate
```

Always commit the generated files alongside your type changes.

### Format and lint

```bash
make fmt   # gofmt
make vet   # go vet
```

### Build the container image

```bash
make docker-build IMG=<registry>/<image>:<tag>
```

For multi-architecture builds (amd64, arm64, s390x, ppc64le):

```bash
make docker-buildx IMG=<registry>/<image>:<tag>
```

### Push the image

```bash
make docker-push IMG=<registry>/<image>:<tag>
```

### Deploy from a custom image

```bash
make deploy IMG=<registry>/<image>:<tag>
```

### Install only the CRD (no controller)

```bash
make install
```

### Uninstall the CRD

```bash
make uninstall
```

---

## Adding a New API Version

The project follows the Kubebuilder multi-version API pattern. To add a new version (e.g., `v1beta1`):

1. Create the new version under `api/v1beta1/`.
2. Add conversion functions (`hub.go` and `conversion.go`) if needed.
3. Register the new version in `main.go`.
4. Annotate the new type with `// +kubebuilder:storageversion` and remove it from the old version.
5. Run `make manifests generate` to regenerate code.

---

## Controller Runtime Flags

The `manager` binary accepts the following flags:

| Flag | Default | Description |
|------|---------|-------------|
| `--metrics-bind-address` | `:8080` | Address for Prometheus metrics endpoint |
| `--health-probe-bind-address` | `:8081` | Address for liveness/readiness probes |
| `--leader-elect` | `false` | Enable leader election for HA deployments |

The default Kustomize deployment enables `--leader-elect=true`.

---

## Testing

Tests are written using [Ginkgo v2](https://onsi.github.io/ginkgo/) and [Gomega](https://onsi.github.io/gomega/) and run against a local Kubernetes API server managed by `envtest`.

### Running a specific test

```bash
go test ./controllers/... -run "TestControllers/should apply labels"
```

### Test timeout

Each test assertion polls with a 250ms interval and a 10-second timeout. Adjust these constants in `controllers/suite_test.go` if needed.

### What is tested

| Scenario | Description |
|----------|-------------|
| Finalizer management | Finalizer is added to new NodeGroups |
| Label application | Labels from spec are applied to member nodes |
| Taint application | Taints from spec are applied to member nodes |
| Cleanup on deletion | Labels and taints are removed when NodeGroup is deleted |
| Label/taint persistence | Re-applied if manually removed from a node |
| Node lifecycle | Labels/taints restored when a node is recreated |
