# elx-nodegroup-controller

A tiny controller for persisting labels and taints on a list of nodes.


## Deploy controller and CRD

```bash
kustomize build config/default | kubectl apply -f -
```

## Sample nodegroup manifest

```yml
apiVersion: k8s.elx.cloud/v1alpha2
kind: NodeGroup
metadata:
  name: nodegroup-sample
spec:
  members: 
    - node1 # Kubernetes node name
  nodeGroupNames: 
    - node0 # Kubernetes nodegroup name, used for clusters with dynamic node naming
  labels:
    name: value
  taints:
    - effect: "NoSchedule"
      key: key
      value: value
```
