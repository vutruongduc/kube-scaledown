# kube-scaledown — Design Specification

**Date:** 2026-04-22
**Status:** Draft
**Author:** vu.truongduc

## Overview

A Go-based Kubernetes controller with a React UI that scales down workloads on a schedule. Rewrite of [hjacobs/kube-downscaler](https://codeberg.org/hjacobs/kube-downscaler) with native support for Agones Fleets and a web dashboard for configuration.

### Motivation

The existing `py-kube-downscaler` only supports a hardcoded set of resource types (`deployments`, `statefulsets`, `rollouts`, etc.) and has no mechanism to add custom CRDs like Agones Fleets. Configuration is done via a single ConfigMap with no UI. This project replaces it with a more extensible, user-friendly solution.

### Goals

- Scale down Kubernetes workloads (Deployments, StatefulSets, CronJobs, Agones Fleets) on a configurable schedule
- CRD-based configuration that is GitOps-friendly (works with ArgoCD)
- React web UI for schedule management, resource browsing, and activity logs
- Helm chart for easy installation on any Kubernetes cluster
- Extensible architecture for adding new resource types

## Architecture

```
+---------------------------------------------+
|              kube-scaledown                  |
|                                             |
|  +---------------+    +------------------+  |
|  |  Controller   |    |   API Server     |  |
|  |  (Go)         |    |   (Go/Gin)       |  |
|  |               |    |                  |  |
|  |  Watches CRDs |    |  REST API for UI |  |
|  |  Scales       |    |  CRUD on CRDs    |  |
|  |  resources    |    |  Status/logs     |  |
|  +------+--------+    +--------+---------+  |
|         |                      |             |
|         |    +-------------+   |             |
|         +--->| Kubernetes  |<--+             |
|              | API Server  |                 |
|              +-------------+                 |
|                                             |
|  +---------------------------------------+  |
|  |  React Frontend (separate build)      |  |
|  |  - Schedule management                |  |
|  |  - Resource browser                   |  |
|  |  - Exclusion rules                    |  |
|  |  - Scale event history                |  |
|  +---------------------------------------+  |
+---------------------------------------------+
```

### Components

1. **Controller** — Go binary using controller-runtime. Reconciliation loop watches `DownscaleSchedule` CRDs and scales resources accordingly.
2. **API Server** — Go binary using Gin. REST API that reads/writes CRDs via the Kubernetes API. Serves the React frontend as static files in production.
3. **React Frontend** — Separate build, bundled into the API server container image for production deployment.

Both controller and API server deploy as separate containers in the same pod (or separate deployments).

## Custom Resource Definition

### DownscaleSchedule

```yaml
apiVersion: downscaler.sipher.gg/v1alpha1
kind: DownscaleSchedule
metadata:
  name: working-hours
spec:
  # Schedule (timezone-aware)
  uptime: "Mon-Sat 08:00-20:00 Asia/Ho_Chi_Minh"
  downtime: "Mon-Sat 20:00-08:00 Asia/Ho_Chi_Minh, Sun 00:00-24:00 Asia/Ho_Chi_Minh"

  # What to scale down to (default: 0)
  downtimeReplicas: 0

  # Resource types to manage
  includeResources:
    - deployments
    - statefulsets
    - fleets.agones.dev

  # Namespace targeting
  includeNamespaces: []       # empty = all namespaces
  excludeNamespaces:
    - kube-system
    - argocd
    - cert-manager

  # Individual resource exclusions
  excludeResources:
    - name: "qa-daily"
      namespace: default
      kind: Fleet

  # Annotation-based opt-out (on the resource itself)
  # downscaler.sipher.gg/exclude: "true"
```

### Status Subresource

Auto-populated by the controller:

```yaml
status:
  lastScaleDown: "2026-04-22T20:00:00+07:00"
  lastScaleUp: "2026-04-22T08:00:00+07:00"
  currentState: "uptime"  # uptime | downtime
  managedResources: 42
  scaledDownResources: 0
```

Multiple `DownscaleSchedule` resources can coexist (e.g., one for services, one for game servers with different schedules).

### Conflict Resolution

If multiple `DownscaleSchedule` CRDs match the same resource, the **most restrictive schedule wins** — if any schedule says "downtime", the resource scales down. This prevents accidental uptime from a broad schedule overriding a specific one.

## Supported Resource Types

| Resource    | API Group      | Scale Method           |
|-------------|----------------|------------------------|
| Deployment  | `apps/v1`      | `spec.replicas -> 0`   |
| StatefulSet | `apps/v1`      | `spec.replicas -> 0`   |
| CronJob     | `batch/v1`     | `spec.suspend -> true` |
| Fleet       | `agones.dev/v1`| `spec.replicas -> 0`   |

### Original Replica Preservation

Before scaling down, the controller stores the original replica count as an annotation:

```
downscaler.sipher.gg/original-replicas: "3"
```

On scale-up, it reads this annotation and restores the original count. If the annotation is missing (e.g., resource created during downtime), it defaults to `1`.

### Resource Opt-Out

Any resource can be excluded by adding an annotation:

```
downscaler.sipher.gg/exclude: "true"
```

### Extensibility

The controller uses an interface-based design so new resource types can be added:

```go
type Scaler interface {
    GetReplicas(obj client.Object) (int32, error)
    SetReplicas(obj client.Object, replicas int32) error
    GetObjectList() client.ObjectList
}
```

Each resource type implements this interface. Adding a new resource type means:
1. Implement the `Scaler` interface
2. Register it in the controller's resource registry

## React Frontend

### Pages

1. **Dashboard** — overview of all schedules, current state (uptime/downtime), counts of managed/scaled resources
2. **Schedules** — CRUD for `DownscaleSchedule` CRDs, visual timeline showing uptime/downtime windows
3. **Resources** — browse all managed resources grouped by namespace/type, see current replica count vs original, manual override (force scale up/down)
4. **Activity Log** — history of scale events with timestamps, resource name, from/to replicas

### API Server Endpoints (Go/Gin)

```
GET    /api/v1/schedules          - list all DownscaleSchedule CRDs
POST   /api/v1/schedules          - create schedule
PUT    /api/v1/schedules/:name    - update schedule
DELETE /api/v1/schedules/:name    - delete schedule
GET    /api/v1/resources          - list managed resources + status
POST   /api/v1/resources/:id/override - manual scale override
GET    /api/v1/events             - scale event history
GET    /api/v1/status             - controller health + summary
```

### Authentication

The UI runs inside the cluster, exposed via ingress. Auth deferred to ingress-level (e.g., basic auth, OAuth proxy) — no built-in auth in v1.

## Helm Chart

The project includes a Helm chart for installation on any Kubernetes cluster.

### Chart Structure

```
charts/kube-scaledown/
  Chart.yaml
  values.yaml
  templates/
    deployment-controller.yaml
    deployment-api.yaml
    service.yaml
    ingress.yaml
    serviceaccount.yaml
    clusterrole.yaml
    clusterrolebinding.yaml
    crd-downscaleschedule.yaml
```

### Default values.yaml

```yaml
controller:
  image:
    repository: ghcr.io/sipherxyz/kube-scaledown-controller
    tag: latest
  resources:
    requests:
      cpu: 50m
      memory: 128Mi
    limits:
      cpu: 200m
      memory: 256Mi
  reconcileInterval: 60  # seconds

api:
  image:
    repository: ghcr.io/sipherxyz/kube-scaledown-api
    tag: latest
  resources:
    requests:
      cpu: 50m
      memory: 128Mi
    limits:
      cpu: 200m
      memory: 256Mi

ingress:
  enabled: false
  className: ""
  hostname: ""
  tls:
    enabled: false
    secretName: ""

serviceAccount:
  create: true
  name: kube-scaledown
```

## Project Structure

```
kube-scaledown/
  cmd/
    controller/           # Controller binary entrypoint
      main.go
    api/                  # API server binary entrypoint
      main.go
  internal/
    controller/           # Reconciler logic
      reconciler.go
      schedule.go         # Schedule parsing (timezone-aware)
    scaler/               # Resource scaling implementations
      interface.go        # Scaler interface
      deployment.go
      statefulset.go
      cronjob.go
      fleet.go            # Agones Fleet scaler
    api/                  # Gin API handlers
      handlers.go
      routes.go
  api/
    v1alpha1/             # CRD types
      types.go
      zz_generated.deepcopy.go
  config/
    crd/                  # CRD manifests
    rbac/                 # RBAC manifests
    samples/              # Example DownscaleSchedule YAMLs
  charts/
    kube-scaledown/       # Helm chart
  frontend/               # React app
    src/
      pages/
        Dashboard.tsx
        Schedules.tsx
        Resources.tsx
        ActivityLog.tsx
      components/
      api/                # API client
    package.json
  Dockerfile.controller
  Dockerfile.api
  Makefile
  go.mod
  docs/
    design.md
```

## Reconciliation Logic

The controller runs a reconciliation loop every `reconcileInterval` seconds (default: 60):

1. List all `DownscaleSchedule` CRDs
2. For each schedule, determine current state (uptime or downtime) based on the schedule spec and current time
3. List all resources matching the schedule's `includeResources` and `includeNamespaces`/`excludeNamespaces`
4. Filter out excluded resources (by `excludeResources` list and `downscaler.sipher.gg/exclude` annotation)
5. For each matched resource:
   - **If downtime and resource is scaled up:** save current replicas to annotation, scale to `downtimeReplicas`
   - **If uptime and resource is scaled down (has original-replicas annotation):** restore original replicas, remove annotation
   - **Otherwise:** no action
6. Update the `DownscaleSchedule` status subresource with current counts and timestamps
7. Emit Kubernetes events for scale operations

## Schedule Format

Reuses the format from the original kube-downscaler for familiarity:

```
<weekday-range> <HH:MM>-<HH:MM> <timezone>
```

Examples:
- `Mon-Fri 08:00-20:00 Asia/Ho_Chi_Minh` — weekdays 8am-8pm BKK
- `Mon-Sat 08:00-20:00 Asia/Ho_Chi_Minh` — Mon through Sat 8am-8pm BKK
- `Sun 00:00-24:00 Asia/Ho_Chi_Minh` — all day Sunday

Multiple rules are comma-separated. Downtime is everything not covered by uptime (or explicitly specified).

## Migration from py-kube-downscaler

For AKS G1, the migration path:

1. Install kube-scaledown via Helm alongside py-kube-downscaler
2. Create a `DownscaleSchedule` CRD matching current ConfigMap values
3. Verify kube-scaledown is scaling resources correctly
4. Remove py-kube-downscaler ArgoCD app
5. Clean up old ConfigMap, RBAC, ServiceAccount
