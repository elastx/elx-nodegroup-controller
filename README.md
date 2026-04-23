# elx-nodegroup-controller

A Kubernetes controller that automatically applies and persists labels and taints across groups of nodes. Define a `NodeGroup` resource and the controller will ensure all member nodes carry the specified labels and taints — even if they are manually removed or if nodes are replaced.

## Table of Contents

- [Overview](#overview)
- [How It Works](#how-it-works)
- [Installation](#installation)
- [Usage](#usage)
  - [NodeGroup API Reference](#nodegroup-api-reference)
  - [Examples](#examples)
- [Development](#development)
- [Architecture](#architecture)

## Overview

Managing labels and taints on Kubernetes nodes is often tedious and error-prone — especially in clusters with dynamic node provisioning where nodes come and go. The `elx-nodegroup-controller` solves this by introducing the `NodeGroup` custom resource, which acts as a declarative specification of which nodes should carry which labels and taints.

Key features:

- **Declarative**: Define the desired state once; the controller continuously reconciles it.
- **Persistent**: Labels and taints are automatically reapplied if manually removed.
- **Cleanup on deletion**: When a `NodeGroup` is deleted, the controller removes the labels and taints it previously applied.
- **Dynamic membership**: Nodes can be selected by exact name or by node group name patterns (useful for auto-scaled node groups).

## How It Works

1. You create a `NodeGroup` resource listing the nodes (by name or naming pattern) and the labels/taints you want applied.
2. The controller watches both `NodeGroup` and `Node` resources.
3. On each reconciliation loop, the controller ensures every member node has the specified labels and taints.
4. If a `NodeGroup` is deleted, the controller cleans up all labels and taints it originally applied via a finalizer before allowing the resource to be removed.

## Installation

### Prerequisites

- Kubernetes cluster >= 1.24
- `kubectl` configured to talk to your cluster
- `kustomize` (or use `kubectl apply -k`)

### Deploy the Controller and CRD

```bash
kustomize build config/default | kubectl apply -f -
```

This creates the following resources in the `elx-nodegroup-controller-system` namespace:

| Resource | Name |
|----------|------|
| CRD | `nodegroups.k8s.elx.cloud` |
| Namespace | `elx-nodegroup-controller-system` |
| ServiceAccount | `controller-manager` |
| ClusterRole | `manager-role` |
| ClusterRoleBinding | `manager-rolebinding` |
| Deployment | `controller-manager` |

### Uninstall

```bash
kustomize build config/default | kubectl delete -f -
```

## Usage

### NodeGroup API Reference

```
apiVersion: k8s.elx.cloud/v1alpha2
kind: NodeGroup
```

`NodeGroup` is a cluster-scoped resource (no namespace required).

#### Spec

| Field | Type | Description |
|-------|------|-------------|
| `members` | `[]string` | Explicit list of Kubernetes node names to include in this group. |
| `nodeGroupNames` | `[]string` | Node naming patterns for dynamic membership. A node is included if any segment of its name (split on `-`) matches one of these values. |
| `labels` | `map[string]string` | Labels to apply to all member nodes. |
| `taints` | `[]corev1.Taint` | Taints to apply to all member nodes. Each taint has `key`, `value` (optional), and `effect` (`NoSchedule`, `PreferNoSchedule`, or `NoExecute`). |

You can use `members`, `nodeGroupNames`, or both together — their results are combined.

### Examples

#### Apply labels and taints to specific nodes

```yaml
apiVersion: k8s.elx.cloud/v1alpha2
kind: NodeGroup
metadata:
  name: compute-nodes
spec:
  members:
    - worker-node-1
    - worker-node-2
  labels:
    workload-type: compute
    environment: production
  taints:
    - key: workload-type
      value: compute
      effect: NoSchedule
```

#### Dynamic membership via node group name patterns

Useful in clusters where auto-scaling creates nodes with predictable name prefixes. A node named `gpu-a100-abc123` would match the pattern `gpu` because `gpu` is one of the `-`-separated segments of the name.

```yaml
apiVersion: k8s.elx.cloud/v1alpha2
kind: NodeGroup
metadata:
  name: gpu-nodes
spec:
  nodeGroupNames:
    - gpu
  labels:
    hardware: gpu
  taints:
    - key: nvidia.com/gpu
      value: "true"
      effect: NoSchedule
```

#### Mixed: explicit members and dynamic patterns

```yaml
apiVersion: k8s.elx.cloud/v1alpha2
kind: NodeGroup
metadata:
  name: spot-nodes
spec:
  members:
    - specific-spot-node-1
  nodeGroupNames:
    - spot
  labels:
    capacity-type: spot
  taints:
    - key: spot-instance
      value: "true"
      effect: NoExecute
```

#### Common operations

```bash
# Create or update a NodeGroup
kubectl apply -f nodegroup.yaml

# List all NodeGroups in the cluster
kubectl get nodegroups

# Inspect a NodeGroup
kubectl describe nodegroup gpu-nodes

# Delete a NodeGroup (cleans up labels and taints from member nodes)
kubectl delete nodegroup gpu-nodes
```

## Development

### Prerequisites

- Go 1.22+
- Docker
- `controller-gen`
- `kustomize`
- `envtest` (for running tests)

### Build

```bash
make build
```

### Run tests

```bash
make test
```

### Generate CRD manifests and DeepCopy methods

```bash
make manifests
make generate
```

### Build and push the container image

```bash
make docker-build docker-push IMG=<registry>/<image>:<tag>
```

### Deploy from a custom image

```bash
make deploy IMG=<registry>/<image>:<tag>
```

## Architecture

The controller is built with [controller-runtime](https://github.com/kubernetes-sigs/controller-runtime) (Kubebuilder v3 layout).

### Reconciliation flow

```
NodeGroup created/updated
        │
        ▼
 Build member list
 (spec.members + nodes matching spec.nodeGroupNames)
        │
        ▼
 NodeGroup being deleted?
     │         │
    Yes        No
     │          │
     ▼          ▼
 Remove        Add finalizer
 labels/taints (if missing)
 from nodes         │
     │              ▼
     ▼        Apply labels to
 Remove       member nodes
 finalizer         │
                   ▼
             Apply taints to
             member nodes
```

### Watching Nodes

The controller also watches `Node` resources. When a node changes (e.g., it is recreated after being replaced by the autoscaler), the controller looks up which `NodeGroup`s list that node as a member and triggers reconciliation for each of them — ensuring labels and taints are immediately reapplied.

### Finalizer

The controller uses the `k8s.elx.cloud/finalizer` finalizer on each `NodeGroup`. This ensures the controller has the opportunity to clean up labels and taints from member nodes before Kubernetes removes the `NodeGroup` object.
