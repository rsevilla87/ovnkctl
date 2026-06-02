# ovnkctl

A diagnostic and inspection CLI for OVN-Kubernetes. It bridges the gap between Kubernetes objects and OVN data-plane state, eliminating the need to manually `kubectl exec` into OVN pods and run raw `ovn-nbctl`/`ovn-sbctl` commands.

## Installation

### From source

Requires Go 1.26+.

```bash
git clone https://github.com/rsevilla/ovnkctl.git
cd ovnkctl
go build -o ovnkctl .
sudo mv ovnkctl /usr/local/bin/
```

### Verify

```bash
ovnkctl --help
```

`ovnkctl` uses your current kubeconfig by default. Override with `--kubeconfig`, `--context`, or `--namespace` flags, same as `kubectl`.

## Zero-config auto-discovery

`ovnkctl` automatically finds OVN-Kubernetes in your cluster. It scans all namespaces for pods matching the `app=ovnkube-node` label, detects the correct namespace (works with upstream `ovn-kubernetes`, OpenShift `openshift-ovn-kubernetes`, or custom namespaces), and identifies the right containers (`nbdb`, `sbdb`, `northd`, `ovnkube-controller`). You never need to specify pod names or OVN namespaces.

## Multi-zone support

In OVN-Kubernetes interconnect (IC) deployments, each node runs its own NB/SB database zone. By default, `show` commands query **all nodes** and merge the results, giving a complete cluster-wide view. Use `--node <name>` to inspect a single node's zone.

## Commands

All commands support `-o table` (default), `-o json`, and `-o yaml` output formats. The `show` subcommands accept `--node` to query a specific node's OVN zone instead of aggregating all nodes.

### status

Overall health summary of OVN-Kubernetes. Answers "is OVN healthy?" in a single command.

```bash
ovnkctl status
```

Reports pod readiness, NB/SB database status, northd version and status, configuration sync state, chassis/tunnel mesh, and OVN CRD instance counts (EgressIP, EgressFirewall, UserDefinedNetwork, etc.).

### show

List OVN-Kubernetes resources with Kubernetes context.

```bash
# Logical switches, routers, and chassis tunnel mesh (all nodes)
ovnkctl show topology

# Single node's OVN zone
ovnkctl show topology --node worker-1.example.com

# Pods with their OVN logical port, MAC, and IP
ovnkctl show pods
ovnkctl show pods -N openshift-monitoring

# ACLs mapped back to their source NetworkPolicy
ovnkctl show acls
ovnkctl show acls -N openshift-ingress
ovnkctl show acls --policy allow-from-proxy

# OVN load balancers mapped to Kubernetes services
ovnkctl show load-balancers
ovnkctl show load-balancers --service openshift-monitoring/prometheus-k8s

# Logical switch ports with pod and node mapping
ovnkctl show ports
ovnkctl show ports --node worker-1.example.com
```

### inspect

Deep inspection of a single Kubernetes resource's OVN networking state.

```bash
# Everything about a pod's networking: logical port, matching ACLs, LB memberships
ovnkctl inspect pod openshift-monitoring/prometheus-k8s-0

# Node's chassis, Geneve tunnels, OVS bridge layout, pod count
ovnkctl inspect node worker-1.example.com

# How a service is realized: VIP-to-backend mapping across all OVN load balancers
ovnkctl inspect service openshift-monitoring/alertmanager-main

# How a NetworkPolicy translates to OVN ACLs
ovnkctl inspect networkpolicy openshift-ingress/allow-from-router
```

### trace

Trace packet flow between pods or to external IPs. Wraps `ovnkube-trace` with automatic pod resolution.

```bash
ovnkctl trace --src default/client-pod --dst default/server-pod --port 8080
ovnkctl trace --src default/client-pod --dst-ip 8.8.8.8 --port 443
ovnkctl trace --src default/client-pod --dst default/server-pod --udp --port 53
```

## Examples

Check cluster health:

```
$ ovnkctl status
OVN-Kubernetes Status
=====================

Namespace:       openshift-ovn-kubernetes
Northd Version:  25.03.2-20.41.0-78.8
Northd:          running
NB DB:           status: ok (healthy)
SB DB:           status: ok (healthy)
Config Sync:     in sync (gen 13919)

Control Plane Pods (2):
NAME                                    NODE          READY
ovnkube-control-plane-747df75d87-hbbxr  cp-1.example  Ready
ovnkube-control-plane-747df75d87-v6v9c  cp-3.example  Ready

Node Pods (3):
NAME                NODE          READY
ovnkube-node-2r4s8  cp-3.example  Ready
ovnkube-node-7rs44  cp-1.example  Ready
ovnkube-node-vqvc2  cp-2.example  Ready

Chassis (3):
HOSTNAME      TUNNEL IP
cp-3.example  10.0.18.169
cp-2.example  10.0.28.101
cp-1.example  10.0.29.45
```

Inspect a service's OVN load balancer:

```
$ ovnkctl inspect service openshift-monitoring/alertmanager-main
Service: openshift-monitoring/alertmanager-main
  ClusterIP: 172.30.238.131
  Type:      ClusterIP

OVN Load Balancers (3):
LB NAME                                                     PROTOCOL  VIP                  BACKENDS
Service_openshift-monitoring/alertmanager-main_TCP_cluster  tcp       172.30.238.131:9092  10.129.0.62:9092,10.130.0.65:9092
Service_openshift-monitoring/alertmanager-main_TCP_cluster  tcp       172.30.238.131:9094  10.129.0.62:9095,10.130.0.65:9095
Service_openshift-monitoring/alertmanager-main_TCP_cluster  tcp       172.30.238.131:9097  10.129.0.62:9097,10.130.0.65:9097
```

Get JSON output for scripting:

```bash
ovnkctl status -o json
ovnkctl show pods -o json | jq '.[] | select(.namespace == "default")'
ovnkctl inspect pod default/my-app -o yaml
```

## How it works

`ovnkctl` connects to the Kubernetes API using your kubeconfig and executes OVN commands inside the existing `ovnkube-node` pods via the pod exec API. It queries the NB database through the `nbdb` container and the SB database through the `sbdb` container. No direct network access to OVN databases is required, and no additional RBAC configuration is needed beyond standard pod exec permissions.

## Requirements

- Kubernetes cluster with OVN-Kubernetes as the CNI
- `kubectl` access with permissions to exec into pods in the OVN namespace
- Works with upstream OVN-Kubernetes and OpenShift OVN-Kubernetes
