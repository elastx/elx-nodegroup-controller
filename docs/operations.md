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

### NetworkPolicy

The controller listens on two ports:

| Port | Purpose | Expected callers |
|------|---------|-----------------|
| 8080 | Metrics (behind `kube-rbac-proxy`) | Prometheus scraper |
| 8081 | Health probes (`/healthz`, `/readyz`) | Kubelet |

Restricting ingress to these ports reduces the blast radius if another workload in the cluster is compromised. Because every cluster has a different Prometheus deployment topology (namespace, label selectors, and whether it runs inside or outside the cluster), no single NetworkPolicy can be shipped with the controller. The templates below cover the common cases — adapt the `namespaceSelector` / `podSelector` blocks to match your environment.

**Deny all ingress by default, then allow only what is needed:**

```yaml
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: elx-nodegroup-controller-manager
  namespace: elx-nodegroup-controller-system
spec:
  podSelector:
    matchLabels:
      control-plane: controller-manager
  policyTypes:
    - Ingress

  ingress:
    # Kubelet health probes (liveness / readiness on port 8081)
    - ports:
        - port: 8081
          protocol: TCP

    # Prometheus metrics scrape (port 8080 via kube-rbac-proxy).
    # Replace the selectors below with whatever matches your Prometheus pod(s).
    # Common variants are shown — uncomment the one that fits your setup.
    - ports:
        - port: 8080
          protocol: TCP
      from:
        # Option A: Prometheus in a dedicated namespace with a known label
        # - namespaceSelector:
        #     matchLabels:
        #       kubernetes.io/metadata.name: monitoring
        #   podSelector:
        #     matchLabels:
        #       app.kubernetes.io/name: prometheus

        # Option B: kube-prometheus-stack default labels
        # - namespaceSelector:
        #     matchLabels:
        #       kubernetes.io/metadata.name: monitoring
        #   podSelector:
        #     matchLabels:
        #       app.kubernetes.io/name: prometheus
        #       operator.prometheus.io/name: kube-prometheus-stack-prometheus

        # Option C: Any pod in a namespace labelled "monitoring=true"
        # - namespaceSelector:
        #     matchLabels:
        #       monitoring: "true"
```

> **Note on egress:** The controller makes outbound calls only to the Kubernetes API server. If your cluster enforces egress policies, ensure the controller namespace has a policy permitting TCP to the API server address (typically port 443 or 6443). The default installation does not include an egress NetworkPolicy.

**Verify the policy is not blocking health probes** after applying:

```bash
kubectl -n elx-nodegroup-controller-system get pods
# Both containers should remain Ready; if the pod becomes unready, check that
# the kubelet CIDR is not blocked and review:
kubectl describe networkpolicy elx-nodegroup-controller-manager -n elx-nodegroup-controller-system
```
