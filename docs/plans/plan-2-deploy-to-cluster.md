# Plan 2: Deploy to Cluster

## Goal

Deploy kube-scaledown to the G1 AKS cluster so it runs continuously without a local laptop.
The controller watches `DownscaleSchedule` CRDs cluster-wide and manages Fleets (with their
FleetAutoscalers), Deployments, StatefulSets, and CronJobs on schedule.

## Context

- Registry: `atherlabs.azurecr.io`
- Cluster: G1 AKS (`g1-admin` context)
- ArgoCD: `az-cd.sipher.gg` (dev)
- Namespace for the controller: `kube-scaledown-system` (already in kustomize default)
- Kustomize manifests are already scaffolded under `config/`
- RBAC `role.yaml` needs one addition: `autoscaling.agones.dev/fleetautoscalers` (create/delete/get/update)
- `Dockerfile` is ready but uses `golang:1.25`; update to `golang:1.26`
- `config/manager/manager.yaml` image is placeholder `controller:latest`

## Tasks

### Task 1: Fix Dockerfile and RBAC

**Dockerfile** — update Go version and set correct image tag variable:
- Change `FROM golang:1.25` → `FROM golang:1.26`
- No other changes needed

**RBAC `config/rbac/role.yaml`** — add `autoscaling.agones.dev` rule for FleetAutoscalers
(the controller deletes and recreates them during Fleet scaledown/scaleup):

```yaml
- apiGroups:
  - autoscaling.agones.dev
  resources:
  - fleetautoscalers
  verbs:
  - create
  - delete
  - get
  - list
  - update
  - watch
```

Regenerate with `make manifests` after editing the controller markers, or edit `role.yaml` directly.

**Verify**: `go build ./...` still passes.

### Task 2: Build and Push Docker Image

Use the existing `Makefile` docker targets. The image tag convention is `<registry>/<repo>:<version>`.

Target image: `atherlabs.azurecr.io/kube-scaledown:0.1.0`

Steps:
1. Log in to ACR: `az acr login --name atherlabs`
2. Build multi-arch image (arm64 + amd64 — G1 nodes are arm64):
   ```
   make docker-buildx IMG=atherlabs.azurecr.io/kube-scaledown:0.1.0
   ```
   If `docker-buildx` target doesn't exist, fall back to:
   ```
   docker buildx build --platform linux/arm64,linux/amd64 \
     -t atherlabs.azurecr.io/kube-scaledown:0.1.0 --push .
   ```
3. Verify push: `az acr repository show-tags --name atherlabs --repository kube-scaledown`

### Task 3: Update Kustomize Manifests

**`config/manager/manager.yaml`** — set real image and tune resources:
- Replace `image: controller:latest` → `image: atherlabs.azurecr.io/kube-scaledown:0.1.0`
- Keep existing resource limits (`cpu: 500m, memory: 128Mi`) — appropriate for a controller

**`config/default/kustomization.yaml`** — strip unused patches:
- Remove the `patches` block referencing `manager_metrics_patch.yaml` (disables metrics TLS
  for now — simpler deployment, no cert-manager dependency)
- Keep: `../crd`, `../rbac`, `../manager`

**`config/manager/manager.yaml`** — remove `metrics_service.yaml` from default kustomization
(metrics endpoint adds complexity; add back later if needed).

Verify rendered output looks correct:
```
kubectl kustomize config/default
```

### Task 4: Add imagePullSecret for ACR

G1 cluster nodes pull from `atherlabs.azurecr.io`. Check if an ACR pull secret already exists
in `kube-scaledown-system` namespace; if not, create one:

```bash
kubectl create namespace kube-scaledown-system --dry-run=client -o yaml | kubectl apply -f -

kubectl create secret docker-registry acr-pull \
  --namespace kube-scaledown-system \
  --docker-server=atherlabs.azurecr.io \
  --docker-username=<sp-client-id> \
  --docker-password=<sp-client-secret>
```

Then reference it in `config/manager/manager.yaml`:
```yaml
imagePullSecrets:
- name: acr-pull
```

If the cluster already has ACR integration (managed identity), skip secret creation and just
verify a test pod can pull from the registry.

### Task 5: Deploy via kubectl and Smoke Test

Apply manifests directly first (before ArgoCD) to verify everything works:

```bash
kubectl apply -k config/default
```

Check the controller starts:
```bash
kubectl -n kube-scaledown-system get pods
kubectl -n kube-scaledown-system logs -l control-plane=controller-manager -f
```

Smoke test — apply the working-hours sample and verify it reconciles:
```bash
kubectl apply -f config/samples/downscaleschedule_working_hours.yaml
kubectl get downscaleschedule -A
```

Expected: controller logs reconcile events, CRD status shows `currentState`.

### Task 6: Wire into ArgoCD

Create an ArgoCD Application that tracks the `config/default` kustomize overlay from the
`kube-scaledown` repo:

```bash
argocd app create kube-scaledown \
  --repo https://github.com/sipherxyz/kube-scaledown.git \
  --path config/default \
  --dest-server https://g1-0ydlt3a5.hcp.southeastasia.azmk8s.io:443 \
  --dest-namespace kube-scaledown-system \
  --sync-policy automated \
  --auto-prune \
  --self-heal \
  --grpc-web
```

Verify in ArgoCD UI at `https://az-cd.sipher.gg` that the app is `Synced` and `Healthy`.

## Verification Checklist

- [ ] `kubectl -n kube-scaledown-system get pods` shows `Running`
- [ ] Controller logs show `Starting workers` with no errors
- [ ] `kubectl get downscaleschedule -A` works (CRD installed)
- [ ] Apply a test `DownscaleSchedule` in downtime → Fleet scales to 0, FAS deleted
- [ ] Update schedule to uptime → Fleet scales back, FAS recreated
- [ ] ArgoCD app shows `Synced` and `Healthy`
- [ ] Kill the controller pod manually → ArgoCD/Deployment restarts it automatically

## Notes

- The `config/default` kustomization deploys to namespace `kube-scaledown-system` with
  name prefix `kube-scaledown-`. The controller `ClusterRole` is cluster-scoped so it can
  watch resources in all namespaces.
- Leader election is enabled (`--leader-elect` flag) — safe for single replica but required
  if we ever scale to 2 replicas for HA.
- The `autoscaling.agones.dev/fleetautoscalers` RBAC permission is required or the controller
  will get 403 errors when deleting/creating FleetAutoscalers during Fleet scaledown/scaleup.
