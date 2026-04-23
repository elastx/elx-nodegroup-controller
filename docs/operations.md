# Operations Guide

## Deployment

### Install

Deploy the controller and CRD with a single command:

```bash
kustomize build config/default | kubectl apply -f -
```

Verify the controller is running:

```bash
kubectl -n elx-nodegroup-controller-system get pods
```

Expected output:

```
NAME                                   READY   STATUS    RESTARTS   AGE
controller-manager-<hash>              2/2     Running   0          30s
```

### Upgrade

Apply the updated manifests:

```bash
kustomize build config/default | kubectl apply -f -
```

The controller deployment uses `RollingUpdate` by default, so upgrades are non-disruptive.

### Uninstall

```bash
kustomize build config/default | kubectl delete -f -
```

> **Note:** Delete all `NodeGroup` resources before uninstalling to ensure the controller can clean up labels and taints from your nodes. If you uninstall the controller while `NodeGroup` resources exist, the nodes will keep the labels and taints but you will need to remove them manually.

---

## Managing NodeGroups

### Create

```bash
kubectl apply -f my-nodegroup.yaml
```

### List

```bash
kubectl get nodegroups
```

### Inspect

```bash
kubectl describe nodegroup <name>
```

### Update

Edit the spec and reapply:

```bash
kubectl apply -f my-nodegroup.yaml
```

Or edit in place:

```bash
kubectl edit nodegroup <name>
```

### Delete

```bash
kubectl delete nodegroup <name>
```

When a `NodeGroup` is deleted, the controller automatically removes the labels and taints it applied from all member nodes before the resource is removed from the cluster.

---

## Monitoring

### Metrics

The controller exposes Prometheus metrics on port `8080` (behind `kube-rbac-proxy` for authentication). Metrics follow the standard `controller-runtime` naming conventions:

- `controller_runtime_reconcile_total` — total number of reconciliations
- `controller_runtime_reconcile_errors_total` — total number of reconciliation errors
- `controller_runtime_reconcile_time_seconds` — reconciliation duration histogram

### Health Probes

The controller exposes health probes on port `8081`:

| Endpoint | Purpose |
|----------|---------|
| `/healthz` | Liveness probe |
| `/readyz` | Readiness probe |

### Logs

Fetch controller logs:

```bash
kubectl -n elx-nodegroup-controller-system logs -l control-plane=controller-manager -f
```

The controller uses structured (JSON) logging via `logr`/`zap`.

---

## Troubleshooting

### Labels or taints not applied

1. Check controller logs for errors:
   ```bash
   kubectl -n elx-nodegroup-controller-system logs -l control-plane=controller-manager
   ```

2. Verify the `NodeGroup` resource is valid:
   ```bash
   kubectl describe nodegroup <name>
   ```

3. Confirm node names in `spec.members` match actual node names exactly:
   ```bash
   kubectl get nodes
   ```

4. For `nodeGroupNames`, verify the segment matches by inspecting node names:
   ```bash
   kubectl get nodes -o name | sed 's|node/||' | tr '-' '\n' | sort -u
   ```

### NodeGroup stuck in deletion

If a `NodeGroup` is stuck with a deletion timestamp and is not being removed, the controller may not be running or may be failing to process the finalization. Check:

```bash
# Is the controller running?
kubectl -n elx-nodegroup-controller-system get pods

# Are there errors?
kubectl -n elx-nodegroup-controller-system logs -l control-plane=controller-manager

# What finalizers are present?
kubectl get nodegroup <name> -o jsonpath='{.metadata.finalizers}'
```

If the controller is permanently unavailable and you need to force-delete the resource (accepting that labels/taints on nodes will **not** be cleaned up):

```bash
kubectl patch nodegroup <name> -p '{"metadata":{"finalizers":[]}}' --type=merge
```

---

## Security Considerations

The controller runs with the minimum required RBAC permissions:

- **Nodes**: `get`, `list`, `watch`, `update`, `patch`
- **NodeGroups**: full CRUD + status + finalizers

The controller pod is configured with:

- Non-root user (`uid: 65532`)
- Read-only root filesystem
- No privilege escalation
- Distroless base image (minimal attack surface)
- Tolerations for all `NoSchedule` taints (so the controller can run even on tainted nodes)
- Node affinity that avoids control-plane nodes
