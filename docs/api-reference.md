# NodeGroup API Reference

## Overview

The `NodeGroup` resource is a cluster-scoped custom resource provided by the `k8s.elx.cloud` API group. It defines a group of nodes and the labels and taints that should be applied to them.

```
Group:   k8s.elx.cloud
Version: v1alpha2
Kind:    NodeGroup
Scope:   Cluster
```

> **Note:** `v1alpha1` is also supported for backwards compatibility but is deprecated. Use `v1alpha2` for all new resources.

---

## Spec

### `spec.members`

**Type:** `[]string`  
**Required:** No

A list of Kubernetes node names to explicitly include in this group.

```yaml
spec:
  members:
    - worker-node-1
    - worker-node-2
```

Each entry must be an exact match of the node's `.metadata.name`.

---

### `spec.nodeGroupNames`

**Type:** `[]string`  
**Required:** No

A list of name segments to use for dynamic node discovery. The controller will include any node in the cluster whose name contains one of these segments when the node name is split on the `-` character.

```yaml
spec:
  nodeGroupNames:
    - gpu
    - spot
```

**How matching works:**

Given a node named `gpu-a100-abc123`, the name is split into parts: `["gpu", "a100", "abc123"]`. If any part appears in `nodeGroupNames`, the node is added to the group.

This is useful in clusters where node pools use a consistent prefix in their generated names (e.g., `gpu-*`, `spot-*`).

You may use `members` and `nodeGroupNames` simultaneously — the resulting member list is the union of both.

---

### `spec.labels`

**Type:** `map[string]string`  
**Required:** No

Labels to apply to all member nodes. The controller adds these labels and will reapply them if they are manually removed.

```yaml
spec:
  labels:
    workload-type: compute
    environment: production
    team: platform
```

Labels that are not in the spec are not touched by the controller. The controller only removes labels it previously applied when the `NodeGroup` is deleted.

---

### `spec.taints`

**Type:** `[]corev1.Taint`  
**Required:** No

Taints to apply to all member nodes. The controller adds these taints if they are not already present.

```yaml
spec:
  taints:
    - key: workload-type
      value: compute
      effect: NoSchedule
    - key: dedicated
      value: gpu
      effect: NoExecute
```

Each taint has the following fields:

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `key` | string | Yes | The taint key. |
| `value` | string | No | The taint value. |
| `effect` | string | Yes | `NoSchedule`, `PreferNoSchedule`, or `NoExecute`. |
| `timeAdded` | timestamp | No | Set automatically by Kubernetes for `NoExecute` taints. |

---

## Status

The `status` subresource is currently reserved and contains no fields.

---

## Full Example

```yaml
apiVersion: k8s.elx.cloud/v1alpha2
kind: NodeGroup
metadata:
  name: gpu-nodes
spec:
  # Include specific nodes by exact name
  members:
    - gpu-node-static-1

  # Include nodes dynamically by name segment
  nodeGroupNames:
    - gpu

  # Labels to apply to all members
  labels:
    hardware: gpu
    capacity-type: on-demand
    team: ml-platform

  # Taints to apply to all members
  taints:
    - key: nvidia.com/gpu
      value: "true"
      effect: NoSchedule
    - key: dedicated
      value: gpu
      effect: NoExecute
```
