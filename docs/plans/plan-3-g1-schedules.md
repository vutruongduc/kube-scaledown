# Plan 3: Wire G1 Schedules and Replace py-kube-downscaler

## Goal

Apply real `DownscaleSchedule` resources to G1 so kube-scaledown manages the cluster's
working-hours downscaling, then disable py-kube-downscaler to remove the redundant tool
and eliminate the FAS-conflict workaround it required.

## Context

- py-kube-downscaler current config (from `kube-downscaler/py-kube-downscaler` ConfigMap):
  - Uptime: `Mon-Sat 08:00-20:00 Asia/Ho_Chi_Minh`
  - Downtime: `Mon-Sat 20:00-08:00 Asia/Ho_Chi_Minh, Sun 00:00-24:00 Asia/Ho_Chi_Minh`
  - `DEFAULT_DOWNTIME_REPLICAS: 1` (scales to 1, not 0)
  - Excluded namespaces: kube-system, kube-downscaler, argocd, cert-manager, ingress-nginx,
    ingress-nginx-internal, monitoring, sipher-system, gatekeeper-system, aks-command, n8n,
    ather-os, unleash, nightshift, s2, pve-tool, trungthuy
- py-kube-downscaler does NOT handle FleetAutoscalers — it uses a separate manual backup
  ConfigMap (`fleet-autoscaler-backup`) as a workaround.
- kube-scaledown handles FAS natively (deletes on scaledown, recreates on scaleup).
- Fleets in `default` namespace: 15 dev game server fleets (`sipher-game-1-0-26-*`).
- kube-scaledown controller is running on G1 in `kube-scaledown-system`, ArgoCD app Synced.
- The sample `config/samples/downscaleschedule_working_hours.yaml` already mirrors the
  py-kube-downscaler schedule with `downtimeReplicas: 0`.

## Tasks

### Task 1: Dry-run Validation

Before applying, verify the controller sees all target resources. Use a test schedule
scoped to a single namespace to confirm FAS delete/recreate works end-to-end:

```bash
# Pick one Fleet with a FAS (e.g. sipher-game-1-0-26-qa-daily) and check its FAS
kubectl --context g1-admin get fleetautoscaler -n default | grep qa-daily

# Apply a test schedule scoped to default namespace only, outside current uptime hours
# (do this after 20:00 HCT or on Sunday)
kubectl --context g1-admin apply -f - <<EOF
apiVersion: downscaler.sipher.gg/v1alpha1
kind: DownscaleSchedule
metadata:
  name: test-default-fleets
  namespace: kube-scaledown-system
spec:
  uptime: "Mon-Sat 08:00-20:00 Asia/Ho_Chi_Minh"
  downtime: "Mon-Sat 20:00-08:00 Asia/Ho_Chi_Minh, Sun 00:00-24:00 Asia/Ho_Chi_Minh"
  downtimeReplicas: 0
  includeResources:
    - fleets.agones.dev
  includeNamespaces:
    - default
EOF

# Check controller logs for reconcile events
kubectl --context g1-admin -n kube-scaledown-system logs -l control-plane=controller-manager -f

# Verify FAS was deleted and Fleet scaled to 0 during downtime
kubectl --context g1-admin get fleets -n default
kubectl --context g1-admin get fleetautoscaler -n default

# Clean up test schedule after validation
kubectl --context g1-admin delete downscaleschedule test-default-fleets -n kube-scaledown-system
```

**Expected**: Fleet scales to 0, FAS deleted. On next uptime: Fleet restores, FAS recreated.

### Task 2: Update Sample Manifest

Update `config/samples/downscaleschedule_working_hours.yaml` to match production intent:
- `downtimeReplicas: 0` (scale fully down, not to 1 like py-kube-downscaler)
- Add `includeResources: cronjobs` (py-kube-downscaler doesn't handle these)
- Namespace: `kube-scaledown-system` (cluster-scoped via ClusterRole, namespace is just for CR placement)

Final manifest:

```yaml
apiVersion: downscaler.sipher.gg/v1alpha1
kind: DownscaleSchedule
metadata:
  name: working-hours
  namespace: kube-scaledown-system
spec:
  uptime: "Mon-Sat 08:00-20:00 Asia/Ho_Chi_Minh"
  downtime: "Mon-Sat 20:00-08:00 Asia/Ho_Chi_Minh, Sun 00:00-24:00 Asia/Ho_Chi_Minh"
  downtimeReplicas: 0
  includeResources:
    - deployments
    - statefulsets
    - fleets.agones.dev
    - cronjobs
  excludeNamespaces:
    - kube-system
    - kube-downscaler
    - kube-scaledown-system
    - argocd
    - cert-manager
    - ingress-nginx
    - ingress-nginx-internal
    - monitoring
    - sipher-system
    - gatekeeper-system
    - aks-command
    - n8n
    - ather-os
    - unleash
    - nightshift
    - s2
    - pve-tool
    - trungthuy
```

### Task 3: Add Sample to ArgoCD App

Add the sample to the kube-scaledown ArgoCD app so it deploys declaratively.
The ArgoCD app currently tracks `config/default`. Add a second app (or patch the existing
one) to also apply `config/samples/`:

```bash
argocd login az-cd.sipher.gg --username admin --password TaTglFu2LeK-njsU --grpc-web

argocd app create kube-scaledown-schedules \
  --repo https://github.com/vutruongduc/kube-scaledown.git \
  --path config/samples \
  --dest-server https://g1-0ydlt3a5.hcp.southeastasia.azmk8s.io:443 \
  --dest-namespace kube-scaledown-system \
  --sync-policy automated \
  --auto-prune \
  --self-heal \
  --grpc-web
```

Verify: `argocd app get kube-scaledown-schedules --grpc-web`

### Task 4: Disable py-kube-downscaler

Once kube-scaledown is confirmed managing the same resources, disable py-kube-downscaler
to avoid double-scaling conflicts. Scale its deployment to 0 (don't delete — easier to
roll back if needed):

```bash
# Check ArgoCD app name for kube-downscaler
argocd app list --grpc-web | grep downscal

# Scale to 0 via ArgoCD helm param override (prevents ArgoCD from restoring it)
argocd app set <app-name> --helm-set replicaCount=0 --grpc-web
argocd app sync <app-name> --grpc-web

# Or directly if not ArgoCD-managed:
kubectl --context g1-admin -n kube-downscaler scale deploy kube-downscaler-py-kube-downscaler --replicas=0
```

**Verify**: `kubectl --context g1-admin -n kube-downscaler get pods` shows no running pods.

### Task 5: Smoke Test Full Cycle

Wait for next downtime window (after 20:00 HCT or Sunday) and verify end-to-end:

```bash
# Check Fleets scaled to 0 and FAS deleted
kubectl --context g1-admin get fleets -n default
kubectl --context g1-admin get fleetautoscaler -n default

# Check Deployments scaled to 0 in non-excluded namespaces
kubectl --context g1-admin get deploy -n ai-review
kubectl --context g1-admin get deploy -n artventure

# Controller logs
kubectl --context g1-admin -n kube-scaledown-system logs -l control-plane=controller-manager --tail=50

# After 08:00 next day: verify scale-up and FAS recreation
kubectl --context g1-admin get fleets -n default
kubectl --context g1-admin get fleetautoscaler -n default
```

Expected `DownscaleSchedule` status:
```bash
kubectl --context g1-admin get downscaleschedule working-hours -n kube-scaledown-system -o yaml | grep -A5 status
```

## Verification Checklist

- [ ] Test schedule (Task 1): Fleet scales to 0, FAS deleted during downtime
- [ ] Test schedule (Task 1): Fleet restores, FAS recreated on uptime
- [ ] Sample manifest updated with `kube-scaledown-system` exclusion (Task 2)
- [ ] ArgoCD `kube-scaledown-schedules` app Synced+Healthy (Task 3)
- [ ] py-kube-downscaler scaled to 0, no conflicts (Task 4)
- [ ] Full overnight cycle: all non-excluded Deployments/Fleets at 0 during downtime (Task 5)
- [ ] Full overnight cycle: all resources restored at 08:00 with FAS recreated (Task 5)

## Notes

- `downtimeReplicas: 0` is more aggressive than py-kube-downscaler's 1. Monitor first
  overnight to ensure no surprises (e.g. resources that should stay at 1 minimum).
  Add `downscaler.sipher.gg/exclude: "true"` annotation to any resource that must stay up.
- The `fleet-autoscaler-backup` ConfigMap in kube-downscaler can be deleted after Task 5
  passes — kube-scaledown's annotation-based FAS backup replaces it.
- Keep py-kube-downscaler deployment at replicas=0 (not deleted) for 1 week before removing
  the ArgoCD app entirely.
