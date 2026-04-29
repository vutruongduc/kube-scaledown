# Remove `downtime` Field from DownscaleSchedule CRD

**Date:** 2026-04-29  
**Status:** Approved

## Problem

The `DownscaleScheduleSpec` has a `downtime` string field that is never read by the controller. The controller only checks `spec.uptime` — if the current time matches, resources scale up; otherwise they scale down. The `downtime` field is dead config that misleads operators into thinking it does something (e.g., setting `downtime: "Mon-Sun 00:00-24:00"` to force everything off has no effect).

## Goal

Remove `downtime` from the CRD so the config surface is minimal and unambiguous: define `uptime`, everything outside it is automatically downtime.

## Changes

### 1. API types (`api/v1alpha1/downscaleschedule_types.go`)

Remove the `Downtime` field from `DownscaleScheduleSpec`:

```go
// Before
type DownscaleScheduleSpec struct {
    Uptime   string `json:"uptime"`
    Downtime string `json:"downtime"`
    ...
}

// After
type DownscaleScheduleSpec struct {
    Uptime string `json:"uptime"`
    ...
}
```

### 2. CRD manifest (`charts/kube-scaledown/crds/downscaler.sipher.gg_downscaleschedules.yaml`)

Remove the `downtime` property from the CRD OpenAPI schema. This file is regenerated via `make generate` or updated manually.

### 3. Helm chart values (`charts/kube-scaledown/values.yaml`)

Remove the `schedule.downtime` key from the default values.

### 4. Helm schedule template (`charts/kube-scaledown/templates/schedule.yaml`)

Remove the `downtime:` line that renders `.Values.schedule.downtime` into the CR.

### 5. G1 values override (`DevOps/kube-scaledown/values-g1.yaml`)

Remove the `downtime:` key. No functional change — it was already ignored.

## Behavior After Change

- Controller behavior is unchanged. Reconciles every 60s, checks `spec.uptime`, scales down within 1 minute after the uptime window ends.
- Existing CRs with a `downtime` field in their stored state are unaffected — Kubernetes ignores unknown fields on read.
- The CRD update does not require a migration — removing an optional field from the schema is non-breaking.

## What Does Not Change

- Controller logic
- Schedule parsing
- All other spec fields (`downtimeReplicas`, `includeResources`, `includeNamespaces`, `excludeNamespaces`, `excludeResources`)
- RBAC, deployment, service account
