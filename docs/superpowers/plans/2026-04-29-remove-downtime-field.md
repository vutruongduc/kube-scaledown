# Remove `downtime` Field Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Remove the unused `downtime` field from the DownscaleSchedule CRD so config has only one schedule field (`uptime`).

**Architecture:** Delete the Go struct field, update the CRD OpenAPI schema, remove it from helm chart values and template, clean up the test fixture, and remove it from the G1 values override in the DevOps repo.

**Tech Stack:** Go, controller-gen/kubebuilder, Helm, Kubernetes CRD

---

## Files Modified

- `api/v1alpha1/downscaleschedule_types.go` — remove `Downtime` field from spec struct
- `charts/kube-scaledown/crds/downscaler.sipher.gg_downscaleschedules.yaml` — remove `downtime` from OpenAPI schema and `required` list
- `charts/kube-scaledown/values.yaml` — remove `schedule.downtime` key
- `charts/kube-scaledown/templates/schedule.yaml` — remove `downtime:` line
- `internal/controller/downscaleschedule_controller_test.go` — remove `Downtime:` from test fixture
- `/Users/vu.truongduc/Projects/DevOpsTools/DevOps/kube-scaledown/values-g1.yaml` (branch `chore/scale-down-g1-until-may4`) — remove `downtime:` key

---

### Task 1: Update test fixture to remove `Downtime` field

**Files:**
- Modify: `internal/controller/downscaleschedule_controller_test.go:56-58`

- [ ] **Step 1: Run existing tests to confirm they pass**

```bash
go test ./internal/controller/... -v 2>&1 | tail -20
```

Expected: all tests pass.

- [ ] **Step 2: Remove `Downtime` from test fixture**

In `internal/controller/downscaleschedule_controller_test.go`, change:

```go
// Before
Spec: downscalerv1alpha1.DownscaleScheduleSpec{
    Uptime:           "Mon-Fri 08:00-18:00 UTC",
    Downtime:         "Mon-Fri 18:00-08:00 UTC",
    DowntimeReplicas: 0,
    IncludeResources: []string{"deployments"},
},

// After
Spec: downscalerv1alpha1.DownscaleScheduleSpec{
    Uptime:           "Mon-Fri 08:00-18:00 UTC",
    DowntimeReplicas: 0,
    IncludeResources: []string{"deployments"},
},
```

- [ ] **Step 3: Verify it fails to compile (Downtime field still exists in struct)**

```bash
go build ./... 2>&1
```

Expected: builds fine (field removal in test is valid even before struct change — just removing an assignment).

---

### Task 2: Remove `Downtime` from the Go struct

**Files:**
- Modify: `api/v1alpha1/downscaleschedule_types.go`

- [ ] **Step 1: Remove the `Downtime` field**

In `api/v1alpha1/downscaleschedule_types.go`, change:

```go
// Before
type DownscaleScheduleSpec struct {
    // Uptime schedule in format "Mon-Fri 08:00-20:00 Asia/Ho_Chi_Minh"
    // +kubebuilder:validation:Required
    Uptime string `json:"uptime"`

    // Downtime schedule in format "Mon-Sat 20:00-08:00 Asia/Ho_Chi_Minh, Sun 00:00-24:00 Asia/Ho_Chi_Minh"
    // +kubebuilder:validation:Required
    Downtime string `json:"downtime"`

    // Replica count during downtime (default: 0)
    // ...
```

```go
// After
type DownscaleScheduleSpec struct {
    // Uptime schedule in format "Mon-Fri 08:00-20:00 Asia/Ho_Chi_Minh"
    // +kubebuilder:validation:Required
    Uptime string `json:"uptime"`

    // Replica count during downtime (default: 0)
    // ...
```

- [ ] **Step 2: Build to confirm no compile errors**

```bash
go build ./...
```

Expected: exits 0, no errors.

- [ ] **Step 3: Run all tests**

```bash
go test ./... 2>&1 | tail -20
```

Expected: all pass.

- [ ] **Step 4: Commit**

```bash
git add api/v1alpha1/downscaleschedule_types.go internal/controller/downscaleschedule_controller_test.go
git commit -m "remove downtime field from DownscaleScheduleSpec"
```

---

### Task 3: Update the CRD manifest

**Files:**
- Modify: `charts/kube-scaledown/crds/downscaler.sipher.gg_downscaleschedules.yaml`

- [ ] **Step 1: Remove `downtime` from the `properties` block**

Find and remove these lines (lines 57-60):

```yaml
              downtime:
                description: Downtime schedule in format "Mon-Sat 20:00-08:00 Asia/Ho_Chi_Minh,
                  Sun 00:00-24:00 Asia/Ho_Chi_Minh"
                type: string
```

- [ ] **Step 2: Remove `downtime` from the `required` list**

Find and remove `- downtime` from the required list (line 108):

```yaml
            required:
            - downtime      # <-- remove this line
            - includeResources
            - uptime
```

Result:

```yaml
            required:
            - includeResources
            - uptime
```

- [ ] **Step 3: Verify YAML is valid**

```bash
python3 -c "import yaml, open as o; yaml.safe_load(open('charts/kube-scaledown/crds/downscaler.sipher.gg_downscaleschedules.yaml'))" 2>&1 || echo "INVALID"
```

Expected: no output (valid YAML).

- [ ] **Step 4: Commit**

```bash
git add charts/kube-scaledown/crds/downscaler.sipher.gg_downscaleschedules.yaml
git commit -m "remove downtime from CRD schema"
```

---

### Task 4: Update helm chart values and template

**Files:**
- Modify: `charts/kube-scaledown/values.yaml`
- Modify: `charts/kube-scaledown/templates/schedule.yaml`

- [ ] **Step 1: Remove `downtime` from values.yaml**

In `charts/kube-scaledown/values.yaml`, remove the `downtime:` line:

```yaml
# Before
schedule:
  enabled: true
  uptime: "Mon-Sat 08:00-20:00 UTC"
  downtime: "Mon-Sat 20:00-08:00 UTC, Sun 00:00-24:00 UTC"
  downtimeReplicas: 0
  includeResources:
    - deployments
    - statefulsets
  excludeNamespaces:
    - kube-system

# After
schedule:
  enabled: true
  uptime: "Mon-Sat 08:00-20:00 UTC"
  downtimeReplicas: 0
  includeResources:
    - deployments
    - statefulsets
  excludeNamespaces:
    - kube-system
```

- [ ] **Step 2: Remove `downtime:` from schedule template**

In `charts/kube-scaledown/templates/schedule.yaml`, remove the `downtime:` line:

```yaml
# Before
spec:
  uptime: {{ .Values.schedule.uptime | quote }}
  downtime: {{ .Values.schedule.downtime | quote }}
  downtimeReplicas: {{ .Values.schedule.downtimeReplicas }}

# After
spec:
  uptime: {{ .Values.schedule.uptime | quote }}
  downtimeReplicas: {{ .Values.schedule.downtimeReplicas }}
```

- [ ] **Step 3: Lint the helm chart**

```bash
helm lint charts/kube-scaledown/
```

Expected: `1 chart(s) linted, 0 chart(s) failed`

- [ ] **Step 4: Commit**

```bash
git add charts/kube-scaledown/values.yaml charts/kube-scaledown/templates/schedule.yaml
git commit -m "remove downtime from helm chart"
```

---

### Task 5: Remove `downtime` from G1 values override

**Files:**
- Modify: `/Users/vu.truongduc/Projects/DevOpsTools/DevOps/kube-scaledown/values-g1.yaml` (branch `chore/scale-down-g1-until-may4`)

- [ ] **Step 1: Remove `downtime:` line from values-g1.yaml**

```yaml
# Before
schedule:
  uptime: "Mon-Mon 00:00-00:00 Asia/Ho_Chi_Minh"
  downtime: "Mon-Sun 00:00-24:00 Asia/Ho_Chi_Minh"
  includeResources:
    ...

# After
schedule:
  uptime: "Mon-Mon 00:00-00:00 Asia/Ho_Chi_Minh"
  includeResources:
    ...
```

- [ ] **Step 2: Commit and push**

```bash
cd /Users/vu.truongduc/Projects/DevOpsTools/DevOps
git add kube-scaledown/values-g1.yaml
git commit -m "remove downtime from g1 values"
git push origin chore/scale-down-g1-until-may4
```

---

### Task 6: Tag and push

- [ ] **Step 1: Confirm all tests still pass**

```bash
cd /Users/vu.truongduc/Projects/kube-scaledown
go test ./... 2>&1 | tail -10
```

Expected: all pass.

- [ ] **Step 2: Push kube-scaledown repo**

```bash
git push origin master
```
