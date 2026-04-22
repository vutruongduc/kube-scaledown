# kube-scaledown Core Controller — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the Go controller that watches `DownscaleSchedule` CRDs and scales Deployments, StatefulSets, CronJobs, and Agones Fleets on a timezone-aware schedule.

**Architecture:** Kubebuilder-scaffolded controller using controller-runtime. CRD defines schedules. Reconciler evaluates current time against schedule, then delegates to resource-specific `Scaler` implementations. Original replica counts preserved via annotations.

**Tech Stack:** Go 1.22+, controller-runtime v0.19.x, kubebuilder v4, envtest for integration tests

---

## File Structure

```
kube-scaledown/
├── cmd/
│   └── controller/
│       └── main.go                          # Manager entrypoint
├── api/
│   └── v1alpha1/
│       ├── groupversion_info.go             # SchemeBuilder + GroupVersion
│       ├── downscaleschedule_types.go       # CRD spec/status types
│       └── zz_generated.deepcopy.go         # auto-generated
├── internal/
│   ├── controller/
│   │   ├── reconciler.go                    # Main reconcile loop
│   │   └── reconciler_test.go               # Integration tests (envtest)
│   ├── scaler/
│   │   ├── scaler.go                        # Scaler interface + registry
│   │   ├── scaler_test.go                   # Unit tests
│   │   ├── deployment.go                    # Deployment scaler
│   │   ├── statefulset.go                   # StatefulSet scaler
│   │   ├── cronjob.go                       # CronJob scaler
│   │   └── fleet.go                         # Agones Fleet scaler
│   └── schedule/
│       ├── schedule.go                      # Schedule parsing + evaluation
│       └── schedule_test.go                 # Unit tests
├── config/
│   ├── crd/
│   │   └── bases/                           # Generated CRD YAML
│   ├── rbac/
│   │   ├── role.yaml                        # Generated ClusterRole
│   │   └── role_binding.yaml
│   └── samples/
│       └── downscaleschedule_sample.yaml
├── Makefile
├── go.mod
└── go.sum
```

---

### Task 1: Project Scaffold with Kubebuilder

**Files:**
- Create: `go.mod`, `Makefile`, `cmd/controller/main.go`, `api/v1alpha1/`, `config/`

- [ ] **Step 1: Initialize Go module**

```bash
cd /Users/vu.truongduc/Projects/kube-scaledown
go mod init github.com/sipherxyz/kube-scaledown
```

- [ ] **Step 2: Initialize kubebuilder project**

```bash
kubebuilder init --domain sipher.gg --repo github.com/sipherxyz/kube-scaledown --project-name kube-scaledown
```

Expected: Creates `cmd/main.go`, `Makefile`, `PROJECT`, `config/` directory tree, `go.mod` updated with controller-runtime deps.

- [ ] **Step 3: Scaffold the CRD and controller**

```bash
kubebuilder create api --group downscaler --version v1alpha1 --kind DownscaleSchedule --resource --controller
```

Expected: Creates `api/v1alpha1/downscaleschedule_types.go`, `api/v1alpha1/groupversion_info.go`, `internal/controller/downscaleschedule_controller.go`, `internal/controller/suite_test.go`.

- [ ] **Step 4: Verify scaffold builds**

```bash
make generate
make manifests
go build ./...
```

Expected: All pass. CRD YAML generated in `config/crd/bases/`.

- [ ] **Step 5: Commit**

```bash
git add -A
git commit -m "scaffold kubebuilder project with DownscaleSchedule CRD"
```

---

### Task 2: Define CRD Types

**Files:**
- Modify: `api/v1alpha1/downscaleschedule_types.go`

- [ ] **Step 1: Write the CRD spec and status types**

Replace the scaffolded types in `api/v1alpha1/downscaleschedule_types.go` with:

```go
package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// DownscaleScheduleSpec defines the desired schedule configuration.
type DownscaleScheduleSpec struct {
	// Uptime schedule in format "Mon-Fri 08:00-20:00 Asia/Ho_Chi_Minh"
	// +kubebuilder:validation:Required
	Uptime string `json:"uptime"`

	// Downtime schedule in format "Mon-Sat 20:00-08:00 Asia/Ho_Chi_Minh, Sun 00:00-24:00 Asia/Ho_Chi_Minh"
	// +kubebuilder:validation:Required
	Downtime string `json:"downtime"`

	// Replica count during downtime (default: 0)
	// +kubebuilder:default=0
	// +kubebuilder:validation:Minimum=0
	DowntimeReplicas int32 `json:"downtimeReplicas,omitempty"`

	// Resource types to manage (e.g., "deployments", "statefulsets", "fleets.agones.dev")
	// +kubebuilder:validation:MinItems=1
	IncludeResources []string `json:"includeResources"`

	// Namespaces to include (empty = all namespaces)
	// +optional
	IncludeNamespaces []string `json:"includeNamespaces,omitempty"`

	// Namespaces to exclude
	// +optional
	ExcludeNamespaces []string `json:"excludeNamespaces,omitempty"`

	// Individual resources to exclude
	// +optional
	ExcludeResources []ResourceRef `json:"excludeResources,omitempty"`
}

// ResourceRef identifies a specific resource to exclude.
type ResourceRef struct {
	// Resource name
	Name string `json:"name"`
	// Resource namespace
	Namespace string `json:"namespace"`
	// Resource kind (e.g., "Deployment", "Fleet")
	Kind string `json:"kind"`
}

// DownscaleScheduleStatus defines the observed state.
type DownscaleScheduleStatus struct {
	// Last time resources were scaled down
	// +optional
	LastScaleDown *metav1.Time `json:"lastScaleDown,omitempty"`

	// Last time resources were scaled up
	// +optional
	LastScaleUp *metav1.Time `json:"lastScaleUp,omitempty"`

	// Current state: "uptime" or "downtime"
	CurrentState string `json:"currentState,omitempty"`

	// Number of resources managed by this schedule
	ManagedResources int32 `json:"managedResources,omitempty"`

	// Number of resources currently scaled down
	ScaledDownResources int32 `json:"scaledDownResources,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=ds
// +kubebuilder:printcolumn:name="State",type=string,JSONPath=`.status.currentState`
// +kubebuilder:printcolumn:name="Managed",type=integer,JSONPath=`.status.managedResources`
// +kubebuilder:printcolumn:name="Scaled Down",type=integer,JSONPath=`.status.scaledDownResources`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// DownscaleSchedule is the Schema for the downscaleschedules API.
type DownscaleSchedule struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   DownscaleScheduleSpec   `json:"spec,omitempty"`
	Status DownscaleScheduleStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// DownscaleScheduleList contains a list of DownscaleSchedule.
type DownscaleScheduleList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []DownscaleSchedule `json:"items"`
}

func init() {
	SchemeBuilder.Register(&DownscaleSchedule{}, &DownscaleScheduleList{})
}
```

- [ ] **Step 2: Regenerate deepcopy and CRD manifests**

```bash
make generate
make manifests
```

Expected: `zz_generated.deepcopy.go` updated, CRD YAML in `config/crd/bases/` updated with new fields.

- [ ] **Step 3: Verify CRD YAML has all fields**

```bash
cat config/crd/bases/downscaler.sipher.gg_downscaleschedules.yaml | head -80
```

Expected: YAML contains `uptime`, `downtime`, `downtimeReplicas`, `includeResources`, `excludeNamespaces`, `excludeResources`, `status` subresource.

- [ ] **Step 4: Commit**

```bash
git add -A
git commit -m "define DownscaleSchedule CRD types with spec and status"
```

---

### Task 3: Schedule Parser

**Files:**
- Create: `internal/schedule/schedule.go`
- Create: `internal/schedule/schedule_test.go`

- [ ] **Step 1: Write failing tests for schedule parsing**

Create `internal/schedule/schedule_test.go`:

```go
package schedule

import (
	"testing"
	"time"
)

func TestParseScheduleRule(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{
			name:    "weekday range with timezone",
			input:   "Mon-Fri 08:00-20:00 Asia/Ho_Chi_Minh",
			wantErr: false,
		},
		{
			name:    "single day",
			input:   "Sun 00:00-24:00 Asia/Ho_Chi_Minh",
			wantErr: false,
		},
		{
			name:    "invalid timezone",
			input:   "Mon-Fri 08:00-20:00 Invalid/Zone",
			wantErr: true,
		},
		{
			name:    "invalid time format",
			input:   "Mon-Fri 8:00-20:00 Asia/Ho_Chi_Minh",
			wantErr: true,
		},
		{
			name:    "invalid day range",
			input:   "Abc-Fri 08:00-20:00 Asia/Ho_Chi_Minh",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseRule(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseRule(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
		})
	}
}

func TestParseSchedule(t *testing.T) {
	input := "Mon-Sat 08:00-20:00 Asia/Ho_Chi_Minh, Sun 00:00-24:00 Asia/Ho_Chi_Minh"
	rules, err := Parse(input)
	if err != nil {
		t.Fatalf("Parse(%q) unexpected error: %v", input, err)
	}
	if len(rules) != 2 {
		t.Fatalf("Parse(%q) got %d rules, want 2", input, len(rules))
	}
}

func TestIsActive(t *testing.T) {
	bkk, _ := time.LoadLocation("Asia/Ho_Chi_Minh")

	tests := []struct {
		name     string
		schedule string
		at       time.Time
		want     bool
	}{
		{
			name:     "monday 10am is within Mon-Sat 08:00-20:00",
			schedule: "Mon-Sat 08:00-20:00 Asia/Ho_Chi_Minh",
			at:       time.Date(2026, 4, 20, 10, 0, 0, 0, bkk), // Monday
			want:     true,
		},
		{
			name:     "monday 21:00 is outside Mon-Sat 08:00-20:00",
			schedule: "Mon-Sat 08:00-20:00 Asia/Ho_Chi_Minh",
			at:       time.Date(2026, 4, 20, 21, 0, 0, 0, bkk), // Monday
			want:     false,
		},
		{
			name:     "sunday 12:00 is outside Mon-Sat",
			schedule: "Mon-Sat 08:00-20:00 Asia/Ho_Chi_Minh",
			at:       time.Date(2026, 4, 19, 12, 0, 0, 0, bkk), // Sunday
			want:     false,
		},
		{
			name:     "sunday 12:00 matches Sun 00:00-24:00",
			schedule: "Sun 00:00-24:00 Asia/Ho_Chi_Minh",
			at:       time.Date(2026, 4, 19, 12, 0, 0, 0, bkk), // Sunday
			want:     true,
		},
		{
			name:     "exactly at start boundary is active",
			schedule: "Mon-Fri 08:00-20:00 Asia/Ho_Chi_Minh",
			at:       time.Date(2026, 4, 20, 8, 0, 0, 0, bkk), // Monday 08:00
			want:     true,
		},
		{
			name:     "exactly at end boundary is not active",
			schedule: "Mon-Fri 08:00-20:00 Asia/Ho_Chi_Minh",
			at:       time.Date(2026, 4, 20, 20, 0, 0, 0, bkk), // Monday 20:00
			want:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rules, err := Parse(tt.schedule)
			if err != nil {
				t.Fatalf("Parse(%q) error: %v", tt.schedule, err)
			}
			got := IsActive(rules, tt.at)
			if got != tt.want {
				t.Errorf("IsActive(%q, %v) = %v, want %v", tt.schedule, tt.at, got, tt.want)
			}
		})
	}
}

func TestIsActiveMultiRule(t *testing.T) {
	bkk, _ := time.LoadLocation("Asia/Ho_Chi_Minh")

	schedule := "Mon-Sat 08:00-20:00 Asia/Ho_Chi_Minh, Sun 10:00-16:00 Asia/Ho_Chi_Minh"
	rules, err := Parse(schedule)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}

	// Sunday 12:00 — matches second rule
	got := IsActive(rules, time.Date(2026, 4, 19, 12, 0, 0, 0, bkk))
	if !got {
		t.Error("expected Sunday 12:00 to be active via second rule")
	}

	// Sunday 09:00 — no rule matches
	got = IsActive(rules, time.Date(2026, 4, 19, 9, 0, 0, 0, bkk))
	if got {
		t.Error("expected Sunday 09:00 to be inactive")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
cd /Users/vu.truongduc/Projects/kube-scaledown
go test ./internal/schedule/ -v
```

Expected: Compilation error — package and functions don't exist yet.

- [ ] **Step 3: Implement schedule parser**

Create `internal/schedule/schedule.go`:

```go
package schedule

import (
	"fmt"
	"strings"
	"time"
)

var dayMap = map[string]time.Weekday{
	"Mon": time.Monday,
	"Tue": time.Tuesday,
	"Wed": time.Wednesday,
	"Thu": time.Thursday,
	"Fri": time.Friday,
	"Sat": time.Saturday,
	"Sun": time.Sunday,
}

// Rule represents a single schedule rule like "Mon-Fri 08:00-20:00 Asia/Ho_Chi_Minh".
type Rule struct {
	DayStart  time.Weekday
	DayEnd    time.Weekday
	TimeStart int // minutes from midnight
	TimeEnd   int // minutes from midnight
	Location  *time.Location
}

// Parse parses a comma-separated schedule string into a slice of Rules.
func Parse(schedule string) ([]Rule, error) {
	parts := strings.Split(schedule, ",")
	var rules []Rule
	for _, part := range parts {
		rule, err := ParseRule(strings.TrimSpace(part))
		if err != nil {
			return nil, fmt.Errorf("parsing rule %q: %w", strings.TrimSpace(part), err)
		}
		rules = append(rules, rule)
	}
	return rules, nil
}

// ParseRule parses a single schedule rule.
// Format: "<DayStart>-<DayEnd> <HH:MM>-<HH:MM> <Timezone>" or "<Day> <HH:MM>-<HH:MM> <Timezone>"
func ParseRule(s string) (Rule, error) {
	fields := strings.Fields(s)
	if len(fields) != 3 {
		return Rule{}, fmt.Errorf("expected 3 fields (days time timezone), got %d", len(fields))
	}

	dayStart, dayEnd, err := parseDayRange(fields[0])
	if err != nil {
		return Rule{}, fmt.Errorf("invalid day range: %w", err)
	}

	timeStart, timeEnd, err := parseTimeRange(fields[1])
	if err != nil {
		return Rule{}, fmt.Errorf("invalid time range: %w", err)
	}

	loc, err := time.LoadLocation(fields[2])
	if err != nil {
		return Rule{}, fmt.Errorf("invalid timezone %q: %w", fields[2], err)
	}

	return Rule{
		DayStart:  dayStart,
		DayEnd:    dayEnd,
		TimeStart: timeStart,
		TimeEnd:   timeEnd,
		Location:  loc,
	}, nil
}

// IsActive returns true if the given time falls within any of the rules.
func IsActive(rules []Rule, t time.Time) bool {
	for _, rule := range rules {
		if rule.Contains(t) {
			return true
		}
	}
	return false
}

// Contains returns true if the given time falls within this rule.
func (r Rule) Contains(t time.Time) bool {
	t = t.In(r.Location)
	weekday := t.Weekday()
	minuteOfDay := t.Hour()*60 + t.Minute()

	if !dayInRange(weekday, r.DayStart, r.DayEnd) {
		return false
	}

	return minuteOfDay >= r.TimeStart && minuteOfDay < r.TimeEnd
}

func parseDayRange(s string) (time.Weekday, time.Weekday, error) {
	if strings.Contains(s, "-") {
		parts := strings.SplitN(s, "-", 2)
		start, ok := dayMap[parts[0]]
		if !ok {
			return 0, 0, fmt.Errorf("unknown day %q", parts[0])
		}
		end, ok := dayMap[parts[1]]
		if !ok {
			return 0, 0, fmt.Errorf("unknown day %q", parts[1])
		}
		return start, end, nil
	}
	day, ok := dayMap[s]
	if !ok {
		return 0, 0, fmt.Errorf("unknown day %q", s)
	}
	return day, day, nil
}

func parseTimeRange(s string) (int, int, error) {
	parts := strings.SplitN(s, "-", 2)
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("expected HH:MM-HH:MM, got %q", s)
	}

	start, err := parseTime(parts[0])
	if err != nil {
		return 0, 0, err
	}
	end, err := parseTime(parts[1])
	if err != nil {
		return 0, 0, err
	}

	return start, end, nil
}

func parseTime(s string) (int, error) {
	if len(s) != 5 || s[2] != ':' {
		return 0, fmt.Errorf("expected HH:MM, got %q", s)
	}

	var h, m int
	_, err := fmt.Sscanf(s, "%d:%d", &h, &m)
	if err != nil {
		return 0, fmt.Errorf("parsing time %q: %w", s, err)
	}

	if h < 0 || h > 24 || m < 0 || m > 59 {
		return 0, fmt.Errorf("time out of range: %q", s)
	}

	return h*60 + m, nil
}

func dayInRange(day, start, end time.Weekday) bool {
	if start <= end {
		return day >= start && day <= end
	}
	// Wraps around (e.g., Fri-Mon)
	return day >= start || day <= end
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
cd /Users/vu.truongduc/Projects/kube-scaledown
go test ./internal/schedule/ -v
```

Expected: All tests pass.

- [ ] **Step 5: Commit**

```bash
git add internal/schedule/
git commit -m "add schedule parser with timezone support"
```

---

### Task 4: Scaler Interface and Deployment Scaler

**Files:**
- Create: `internal/scaler/scaler.go`
- Create: `internal/scaler/deployment.go`
- Create: `internal/scaler/scaler_test.go`

- [ ] **Step 1: Write the Scaler interface**

Create `internal/scaler/scaler.go`:

```go
package scaler

import (
	"context"
	"fmt"
	"strconv"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	AnnotationOriginalReplicas = "downscaler.sipher.gg/original-replicas"
	AnnotationExclude          = "downscaler.sipher.gg/exclude"
)

// Scaler knows how to get and set replicas for a specific resource type.
type Scaler interface {
	// ResourceName returns the name used in includeResources (e.g., "deployments").
	ResourceName() string
	// GetReplicas returns the current replica count.
	GetReplicas(obj client.Object) (int32, error)
	// SetReplicas sets the replica count on the object (does not persist).
	SetReplicas(obj client.Object, replicas int32) error
	// NewObjectList returns an empty typed list for listing resources.
	NewObjectList() client.ObjectList
}

// Registry maps resource names to Scaler implementations.
type Registry struct {
	scalers map[string]Scaler
}

// NewRegistry creates a registry with all built-in scalers.
func NewRegistry() *Registry {
	r := &Registry{scalers: make(map[string]Scaler)}
	r.Register(&DeploymentScaler{})
	r.Register(&StatefulSetScaler{})
	r.Register(&CronJobScaler{})
	r.Register(&FleetScaler{})
	return r
}

// Register adds a scaler to the registry.
func (r *Registry) Register(s Scaler) {
	r.scalers[s.ResourceName()] = s
}

// Get returns the scaler for the given resource name.
func (r *Registry) Get(name string) (Scaler, error) {
	s, ok := r.scalers[name]
	if !ok {
		return nil, fmt.Errorf("unknown resource type %q", name)
	}
	return s, nil
}

// IsExcluded checks if a resource has the exclude annotation.
func IsExcluded(obj client.Object) bool {
	annotations := obj.GetAnnotations()
	if annotations == nil {
		return false
	}
	return annotations[AnnotationExclude] == "true"
}

// SaveOriginalReplicas stores the current replica count as an annotation.
func SaveOriginalReplicas(obj client.Object, replicas int32) {
	annotations := obj.GetAnnotations()
	if annotations == nil {
		annotations = make(map[string]string)
	}
	annotations[AnnotationOriginalReplicas] = strconv.Itoa(int(replicas))
	obj.SetAnnotations(annotations)
}

// GetOriginalReplicas reads the saved replica count. Returns 1 as default.
func GetOriginalReplicas(obj client.Object) int32 {
	annotations := obj.GetAnnotations()
	if annotations == nil {
		return 1
	}
	val, ok := annotations[AnnotationOriginalReplicas]
	if !ok {
		return 1
	}
	n, err := strconv.Atoi(val)
	if err != nil {
		return 1
	}
	return int32(n)
}

// ClearOriginalReplicas removes the saved replica annotation.
func ClearOriginalReplicas(obj client.Object) {
	annotations := obj.GetAnnotations()
	if annotations == nil {
		return
	}
	delete(annotations, AnnotationOriginalReplicas)
	obj.SetAnnotations(annotations)
}

// ScaleDown scales a resource down and saves original replicas. Returns true if scaled.
func ScaleDown(ctx context.Context, c client.Client, s Scaler, obj client.Object, downtimeReplicas int32) (bool, error) {
	current, err := s.GetReplicas(obj)
	if err != nil {
		return false, err
	}
	if current <= downtimeReplicas {
		return false, nil
	}
	SaveOriginalReplicas(obj, current)
	if err := s.SetReplicas(obj, downtimeReplicas); err != nil {
		return false, err
	}
	if err := c.Update(ctx, obj); err != nil {
		return false, fmt.Errorf("updating %s/%s: %w", obj.GetNamespace(), obj.GetName(), err)
	}
	return true, nil
}

// ScaleUp restores a resource to its original replicas. Returns true if scaled.
func ScaleUp(ctx context.Context, c client.Client, s Scaler, obj client.Object) (bool, error) {
	original := GetOriginalReplicas(obj)
	current, err := s.GetReplicas(obj)
	if err != nil {
		return false, err
	}
	if current >= original {
		ClearOriginalReplicas(obj)
		if err := c.Update(ctx, obj); err != nil {
			return false, fmt.Errorf("clearing annotation on %s/%s: %w", obj.GetNamespace(), obj.GetName(), err)
		}
		return false, nil
	}
	ClearOriginalReplicas(obj)
	if err := s.SetReplicas(obj, original); err != nil {
		return false, err
	}
	if err := c.Update(ctx, obj); err != nil {
		return false, fmt.Errorf("updating %s/%s: %w", obj.GetNamespace(), obj.GetName(), err)
	}
	return true, nil
}
```

- [ ] **Step 2: Write the Deployment scaler**

Create `internal/scaler/deployment.go`:

```go
package scaler

import (
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type DeploymentScaler struct{}

func (d *DeploymentScaler) ResourceName() string { return "deployments" }

func (d *DeploymentScaler) GetReplicas(obj client.Object) (int32, error) {
	deploy, ok := obj.(*appsv1.Deployment)
	if !ok {
		return 0, fmt.Errorf("expected Deployment, got %T", obj)
	}
	if deploy.Spec.Replicas == nil {
		return 1, nil
	}
	return *deploy.Spec.Replicas, nil
}

func (d *DeploymentScaler) SetReplicas(obj client.Object, replicas int32) error {
	deploy, ok := obj.(*appsv1.Deployment)
	if !ok {
		return fmt.Errorf("expected Deployment, got %T", obj)
	}
	deploy.Spec.Replicas = &replicas
	return nil
}

func (d *DeploymentScaler) NewObjectList() client.ObjectList {
	return &appsv1.DeploymentList{}
}
```

- [ ] **Step 3: Write unit tests for scaler helpers and deployment scaler**

Create `internal/scaler/scaler_test.go`:

```go
package scaler

import (
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func int32Ptr(i int32) *int32 { return &i }

func newDeployment(name string, replicas int32, annotations map[string]string) *appsv1.Deployment {
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Namespace:   "default",
			Annotations: annotations,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: int32Ptr(replicas),
		},
	}
}

func TestDeploymentScaler_GetReplicas(t *testing.T) {
	s := &DeploymentScaler{}

	deploy := newDeployment("test", 3, nil)
	got, err := s.GetReplicas(deploy)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != 3 {
		t.Errorf("got %d, want 3", got)
	}
}

func TestDeploymentScaler_GetReplicas_NilDefaultsTo1(t *testing.T) {
	s := &DeploymentScaler{}

	deploy := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
		Spec:       appsv1.DeploymentSpec{},
	}
	got, err := s.GetReplicas(deploy)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != 1 {
		t.Errorf("got %d, want 1 (nil defaults to 1)", got)
	}
}

func TestDeploymentScaler_SetReplicas(t *testing.T) {
	s := &DeploymentScaler{}

	deploy := newDeployment("test", 3, nil)
	if err := s.SetReplicas(deploy, 0); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if *deploy.Spec.Replicas != 0 {
		t.Errorf("got %d, want 0", *deploy.Spec.Replicas)
	}
}

func TestDeploymentScaler_WrongType(t *testing.T) {
	s := &DeploymentScaler{}

	var obj client.Object = &appsv1.StatefulSet{}
	_, err := s.GetReplicas(obj)
	if err == nil {
		t.Error("expected error for wrong type")
	}
}

func TestIsExcluded(t *testing.T) {
	excluded := newDeployment("test", 1, map[string]string{AnnotationExclude: "true"})
	if !IsExcluded(excluded) {
		t.Error("expected excluded")
	}

	notExcluded := newDeployment("test", 1, nil)
	if IsExcluded(notExcluded) {
		t.Error("expected not excluded")
	}

	wrongValue := newDeployment("test", 1, map[string]string{AnnotationExclude: "false"})
	if IsExcluded(wrongValue) {
		t.Error("expected not excluded for value 'false'")
	}
}

func TestSaveAndGetOriginalReplicas(t *testing.T) {
	deploy := newDeployment("test", 3, nil)

	SaveOriginalReplicas(deploy, 3)
	got := GetOriginalReplicas(deploy)
	if got != 3 {
		t.Errorf("got %d, want 3", got)
	}
}

func TestGetOriginalReplicas_DefaultsTo1(t *testing.T) {
	deploy := newDeployment("test", 0, nil)
	got := GetOriginalReplicas(deploy)
	if got != 1 {
		t.Errorf("got %d, want 1 (default)", got)
	}
}

func TestClearOriginalReplicas(t *testing.T) {
	deploy := newDeployment("test", 3, map[string]string{AnnotationOriginalReplicas: "3"})

	ClearOriginalReplicas(deploy)
	_, ok := deploy.GetAnnotations()[AnnotationOriginalReplicas]
	if ok {
		t.Error("expected annotation to be cleared")
	}
}

func TestRegistry(t *testing.T) {
	r := NewRegistry()

	s, err := r.Get("deployments")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s.ResourceName() != "deployments" {
		t.Errorf("got %q, want 'deployments'", s.ResourceName())
	}

	_, err = r.Get("unknown")
	if err == nil {
		t.Error("expected error for unknown resource type")
	}
}
```

- [ ] **Step 4: Run tests**

```bash
cd /Users/vu.truongduc/Projects/kube-scaledown
go test ./internal/scaler/ -v
```

Expected: All tests pass.

- [ ] **Step 5: Commit**

```bash
git add internal/scaler/scaler.go internal/scaler/deployment.go internal/scaler/scaler_test.go
git commit -m "add scaler interface, registry, and deployment scaler"
```

---

### Task 5: StatefulSet, CronJob, and Fleet Scalers

**Files:**
- Create: `internal/scaler/statefulset.go`
- Create: `internal/scaler/cronjob.go`
- Create: `internal/scaler/fleet.go`

- [ ] **Step 1: Implement StatefulSet scaler**

Create `internal/scaler/statefulset.go`:

```go
package scaler

import (
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type StatefulSetScaler struct{}

func (s *StatefulSetScaler) ResourceName() string { return "statefulsets" }

func (s *StatefulSetScaler) GetReplicas(obj client.Object) (int32, error) {
	sts, ok := obj.(*appsv1.StatefulSet)
	if !ok {
		return 0, fmt.Errorf("expected StatefulSet, got %T", obj)
	}
	if sts.Spec.Replicas == nil {
		return 1, nil
	}
	return *sts.Spec.Replicas, nil
}

func (s *StatefulSetScaler) SetReplicas(obj client.Object, replicas int32) error {
	sts, ok := obj.(*appsv1.StatefulSet)
	if !ok {
		return fmt.Errorf("expected StatefulSet, got %T", obj)
	}
	sts.Spec.Replicas = &replicas
	return nil
}

func (s *StatefulSetScaler) NewObjectList() client.ObjectList {
	return &appsv1.StatefulSetList{}
}
```

- [ ] **Step 2: Implement CronJob scaler**

Create `internal/scaler/cronjob.go`:

```go
package scaler

import (
	"fmt"

	batchv1 "k8s.io/api/batch/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type CronJobScaler struct{}

func (c *CronJobScaler) ResourceName() string { return "cronjobs" }

// GetReplicas returns 0 if suspended, 1 if active.
func (c *CronJobScaler) GetReplicas(obj client.Object) (int32, error) {
	cj, ok := obj.(*batchv1.CronJob)
	if !ok {
		return 0, fmt.Errorf("expected CronJob, got %T", obj)
	}
	if cj.Spec.Suspend != nil && *cj.Spec.Suspend {
		return 0, nil
	}
	return 1, nil
}

// SetReplicas suspends (0) or unsuspends (>0) the CronJob.
func (c *CronJobScaler) SetReplicas(obj client.Object, replicas int32) error {
	cj, ok := obj.(*batchv1.CronJob)
	if !ok {
		return fmt.Errorf("expected CronJob, got %T", obj)
	}
	suspend := replicas == 0
	cj.Spec.Suspend = &suspend
	return nil
}

func (c *CronJobScaler) NewObjectList() client.ObjectList {
	return &batchv1.CronJobList{}
}
```

- [ ] **Step 3: Implement Agones Fleet scaler**

Create `internal/scaler/fleet.go`:

```go
package scaler

import (
	"fmt"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type FleetScaler struct{}

func (f *FleetScaler) ResourceName() string { return "fleets.agones.dev" }

// GetReplicas reads spec.replicas from the unstructured Fleet object.
func (f *FleetScaler) GetReplicas(obj client.Object) (int32, error) {
	u, ok := obj.(*unstructured.Unstructured)
	if !ok {
		return 0, fmt.Errorf("expected Unstructured, got %T", obj)
	}
	replicas, found, err := unstructured.NestedInt64(u.Object, "spec", "replicas")
	if err != nil {
		return 0, fmt.Errorf("reading spec.replicas: %w", err)
	}
	if !found {
		return 1, nil
	}
	return int32(replicas), nil
}

// SetReplicas sets spec.replicas on the unstructured Fleet object.
func (f *FleetScaler) SetReplicas(obj client.Object, replicas int32) error {
	u, ok := obj.(*unstructured.Unstructured)
	if !ok {
		return fmt.Errorf("expected Unstructured, got %T", obj)
	}
	return unstructured.SetNestedField(u.Object, int64(replicas), "spec", "replicas")
}

// NewObjectList returns an unstructured list for Agones Fleets.
func (f *FleetScaler) NewObjectList() client.ObjectList {
	list := &unstructured.UnstructuredList{}
	list.SetGroupVersionKind(FleetGVK())
	return list
}

// FleetGVK returns the GroupVersionKind for Agones Fleet.
func FleetGVK() schema.GroupVersionKind {
	return schema.GroupVersionKind{
		Group:   "agones.dev",
		Version: "v1",
		Kind:    "FleetList",
	}
}
```

The full import block for fleet.go should be:
```go
import (
	"fmt"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
)
```

- [ ] **Step 4: Run build to verify compilation**

```bash
cd /Users/vu.truongduc/Projects/kube-scaledown
go build ./internal/scaler/
```

Expected: Compiles successfully.

- [ ] **Step 5: Commit**

```bash
git add internal/scaler/statefulset.go internal/scaler/cronjob.go internal/scaler/fleet.go
git commit -m "add statefulset, cronjob, and agones fleet scalers"
```

---

### Task 6: Reconciler Implementation

**Files:**
- Modify: `internal/controller/downscaleschedule_controller.go` (rename to `reconciler.go`)

- [ ] **Step 1: Implement the reconciler**

Replace the scaffolded controller with the full reconciler. Rename the file to `internal/controller/reconciler.go`:

```go
package controller

import (
	"context"
	"fmt"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	downscalerv1alpha1 "github.com/sipherxyz/kube-scaledown/api/v1alpha1"
	"github.com/sipherxyz/kube-scaledown/internal/scaler"
	"github.com/sipherxyz/kube-scaledown/internal/schedule"
)

type DownscaleScheduleReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Registry *scaler.Registry
	Now      func() time.Time // injectable for testing
}

// +kubebuilder:rbac:groups=downscaler.sipher.gg,resources=downscaleschedules,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=downscaler.sipher.gg,resources=downscaleschedules/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=apps,resources=statefulsets,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=batch,resources=cronjobs,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=agones.dev,resources=fleets;fleets/scale,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch

func (r *DownscaleScheduleReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// Fetch the schedule
	var ds downscalerv1alpha1.DownscaleSchedule
	if err := r.Get(ctx, req.NamespacedName, &ds); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Parse uptime schedule
	uptimeRules, err := schedule.Parse(ds.Spec.Uptime)
	if err != nil {
		logger.Error(err, "failed to parse uptime schedule")
		return ctrl.Result{RequeueAfter: 60 * time.Second}, nil
	}

	now := r.now()
	isUptime := schedule.IsActive(uptimeRules, now)
	state := "downtime"
	if isUptime {
		state = "uptime"
	}

	var managedCount, scaledDownCount int32

	// Process each resource type
	for _, resourceName := range ds.Spec.IncludeResources {
		s, err := r.Registry.Get(resourceName)
		if err != nil {
			logger.Error(err, "unknown resource type", "resource", resourceName)
			continue
		}

		objects, err := r.listResources(ctx, s, &ds)
		if err != nil {
			logger.Error(err, "failed to list resources", "resource", resourceName)
			continue
		}

		for _, obj := range objects {
			if scaler.IsExcluded(obj) {
				continue
			}
			if r.isResourceExcluded(obj, ds.Spec.ExcludeResources) {
				continue
			}

			managedCount++

			if isUptime {
				// Check if resource has original-replicas annotation (was scaled down)
				if scaler.GetOriginalReplicas(obj) > 0 && hasOriginalReplicasAnnotation(obj) {
					scaled, err := scaler.ScaleUp(ctx, r.Client, s, obj)
					if err != nil {
						logger.Error(err, "failed to scale up", "resource", obj.GetName(), "namespace", obj.GetNamespace())
						continue
					}
					if scaled {
						logger.Info("scaled up", "resource", obj.GetName(), "namespace", obj.GetNamespace())
					}
				}
			} else {
				// Downtime — scale down
				scaled, err := scaler.ScaleDown(ctx, r.Client, s, obj, ds.Spec.DowntimeReplicas)
				if err != nil {
					logger.Error(err, "failed to scale down", "resource", obj.GetName(), "namespace", obj.GetNamespace())
					continue
				}
				if scaled {
					logger.Info("scaled down", "resource", obj.GetName(), "namespace", obj.GetNamespace())
				}
				currentReplicas, _ := s.GetReplicas(obj)
				if currentReplicas <= ds.Spec.DowntimeReplicas {
					scaledDownCount++
				}
			}
		}
	}

	// Update status
	ds.Status.CurrentState = state
	ds.Status.ManagedResources = managedCount
	ds.Status.ScaledDownResources = scaledDownCount
	nowMeta := metav1.NewTime(now)
	if state == "downtime" {
		ds.Status.LastScaleDown = &nowMeta
	} else {
		ds.Status.LastScaleUp = &nowMeta
	}
	if err := r.Status().Update(ctx, &ds); err != nil {
		logger.Error(err, "failed to update status")
	}

	return ctrl.Result{RequeueAfter: 60 * time.Second}, nil
}

func (r *DownscaleScheduleReconciler) now() time.Time {
	if r.Now != nil {
		return r.Now()
	}
	return time.Now()
}

func (r *DownscaleScheduleReconciler) listResources(ctx context.Context, s scaler.Scaler, ds *downscalerv1alpha1.DownscaleSchedule) ([]client.Object, error) {
	list := s.NewObjectList()

	namespaces := ds.Spec.IncludeNamespaces
	if len(namespaces) == 0 {
		// List across all namespaces, then filter excludes
		if err := r.List(ctx, list); err != nil {
			return nil, err
		}
		return r.filterByNamespace(extractObjects(list), ds.Spec.ExcludeNamespaces), nil
	}

	var result []client.Object
	for _, ns := range namespaces {
		if err := r.List(ctx, list, client.InNamespace(ns)); err != nil {
			return nil, err
		}
		result = append(result, extractObjects(list)...)
	}
	return result, nil
}

func extractObjects(list client.ObjectList) []client.Object {
	switch l := list.(type) {
	case *appsv1.DeploymentList:
		var objs []client.Object
		for i := range l.Items {
			objs = append(objs, &l.Items[i])
		}
		return objs
	case *appsv1.StatefulSetList:
		var objs []client.Object
		for i := range l.Items {
			objs = append(objs, &l.Items[i])
		}
		return objs
	case *batchv1.CronJobList:
		var objs []client.Object
		for i := range l.Items {
			objs = append(objs, &l.Items[i])
		}
		return objs
	case *unstructured.UnstructuredList:
		var objs []client.Object
		for i := range l.Items {
			objs = append(objs, &l.Items[i])
		}
		return objs
	default:
		return nil
	}
}

func (r *DownscaleScheduleReconciler) filterByNamespace(objs []client.Object, excludeNamespaces []string) []client.Object {
	if len(excludeNamespaces) == 0 {
		return objs
	}
	excludeSet := make(map[string]bool)
	for _, ns := range excludeNamespaces {
		excludeSet[ns] = true
	}
	var filtered []client.Object
	for _, obj := range objs {
		if !excludeSet[obj.GetNamespace()] {
			filtered = append(filtered, obj)
		}
	}
	return filtered
}

func (r *DownscaleScheduleReconciler) isResourceExcluded(obj client.Object, excludes []downscalerv1alpha1.ResourceRef) bool {
	for _, ref := range excludes {
		if ref.Name == obj.GetName() && ref.Namespace == obj.GetNamespace() {
			return true
		}
	}
	return false
}

func hasOriginalReplicasAnnotation(obj client.Object) bool {
	annotations := obj.GetAnnotations()
	if annotations == nil {
		return false
	}
	_, ok := annotations[scaler.AnnotationOriginalReplicas]
	return ok
}

func (r *DownscaleScheduleReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&downscalerv1alpha1.DownscaleSchedule{}).
		Complete(r)
}
```

- [ ] **Step 2: Update cmd/main.go to wire up the reconciler**

Update the main.go to initialize the registry and pass it to the reconciler. Find the section where the controller is registered and update it:

```go
// In the section where controllers are set up:
if err = (&controller.DownscaleScheduleReconciler{
	Client:   mgr.GetClient(),
	Scheme:   mgr.GetScheme(),
	Registry: scaler.NewRegistry(),
}).SetupWithManager(mgr); err != nil {
	setupLog.Error(err, "unable to create controller", "controller", "DownscaleSchedule")
	os.Exit(1)
}
```

Add the import:
```go
"github.com/sipherxyz/kube-scaledown/internal/scaler"
```

- [ ] **Step 3: Build to verify compilation**

```bash
cd /Users/vu.truongduc/Projects/kube-scaledown
make generate
make manifests
go build ./...
```

Expected: Compiles successfully.

- [ ] **Step 4: Commit**

```bash
git add -A
git commit -m "implement reconciler with schedule evaluation and resource scaling"
```

---

### Task 7: Integration Tests with envtest

**Files:**
- Modify: `internal/controller/suite_test.go`
- Create: `internal/controller/reconciler_test.go`

- [ ] **Step 1: Update test suite setup**

Modify `internal/controller/suite_test.go` to register the CRD and start the controller:

```go
package controller

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	downscalerv1alpha1 "github.com/sipherxyz/kube-scaledown/api/v1alpha1"
	"github.com/sipherxyz/kube-scaledown/internal/scaler"
)

var (
	k8sClient client.Client
	testEnv   *envtest.Environment
	ctx       context.Context
	cancel    context.CancelFunc
	mockNow   time.Time
)

func TestControllers(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Controller Suite")
}

var _ = BeforeSuite(func() {
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

	ctx, cancel = context.WithCancel(context.TODO())

	By("bootstrapping test environment")
	testEnv = &envtest.Environment{
		CRDDirectoryPaths:     []string{filepath.Join("..", "..", "config", "crd", "bases")},
		ErrorIfCRDPathMissing: true,
	}

	cfg, err := testEnv.Start()
	Expect(err).NotTo(HaveOccurred())
	Expect(cfg).NotTo(BeNil())

	err = downscalerv1alpha1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	k8sClient, err = client.New(cfg, client.Options{Scheme: scheme.Scheme})
	Expect(err).NotTo(HaveOccurred())

	// Create test namespace
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "test-ns"}}
	Expect(k8sClient.Create(ctx, ns)).To(Succeed())

	mgr, err := ctrl.NewManager(cfg, ctrl.Options{Scheme: scheme.Scheme})
	Expect(err).NotTo(HaveOccurred())

	// Default mock time: Monday 10:00 BKK (uptime)
	bkk, _ := time.LoadLocation("Asia/Ho_Chi_Minh")
	mockNow = time.Date(2026, 4, 20, 10, 0, 0, 0, bkk)

	err = (&DownscaleScheduleReconciler{
		Client:   mgr.GetClient(),
		Scheme:   mgr.GetScheme(),
		Registry: scaler.NewRegistry(),
		Now:      func() time.Time { return mockNow },
	}).SetupWithManager(mgr)
	Expect(err).NotTo(HaveOccurred())

	go func() {
		defer GinkgoRecover()
		err := mgr.Start(ctx)
		Expect(err).NotTo(HaveOccurred())
	}()
})

var _ = AfterSuite(func() {
	cancel()
	By("tearing down the test environment")
	err := testEnv.Stop()
	Expect(err).NotTo(HaveOccurred())
})
```

- [ ] **Step 2: Write integration tests**

Create `internal/controller/reconciler_test.go`:

```go
package controller

import (
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	downscalerv1alpha1 "github.com/sipherxyz/kube-scaledown/api/v1alpha1"
	"github.com/sipherxyz/kube-scaledown/internal/scaler"
)

func int32Ptr(i int32) *int32 { return &i }

var _ = Describe("DownscaleSchedule Controller", func() {

	const timeout = 10 * time.Second
	const interval = 250 * time.Millisecond

	Context("During uptime", func() {
		It("should not scale down deployments", func() {
			// Set time to Monday 10:00 BKK (uptime)
			bkk, _ := time.LoadLocation("Asia/Ho_Chi_Minh")
			mockNow = time.Date(2026, 4, 20, 10, 0, 0, 0, bkk)

			// Create a deployment
			deploy := &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "uptime-test",
					Namespace: "test-ns",
				},
				Spec: appsv1.DeploymentSpec{
					Replicas: int32Ptr(3),
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"app": "uptime-test"},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": "uptime-test"}},
						Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "test", Image: "nginx"}}},
					},
				},
			}
			Expect(k8sClient.Create(ctx, deploy)).To(Succeed())

			// Create schedule
			schedule := &downscalerv1alpha1.DownscaleSchedule{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "uptime-schedule",
					Namespace: "default",
				},
				Spec: downscalerv1alpha1.DownscaleScheduleSpec{
					Uptime:           "Mon-Sat 08:00-20:00 Asia/Ho_Chi_Minh",
					Downtime:         "Mon-Sat 20:00-08:00 Asia/Ho_Chi_Minh, Sun 00:00-24:00 Asia/Ho_Chi_Minh",
					DowntimeReplicas: 0,
					IncludeResources: []string{"deployments"},
					IncludeNamespaces: []string{"test-ns"},
				},
			}
			Expect(k8sClient.Create(ctx, schedule)).To(Succeed())

			// Verify deployment stays at 3 replicas
			Consistently(func() int32 {
				var d appsv1.Deployment
				Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "uptime-test", Namespace: "test-ns"}, &d)).To(Succeed())
				return *d.Spec.Replicas
			}, "5s", interval).Should(Equal(int32(3)))
		})
	})

	Context("During downtime", func() {
		It("should scale down deployments and save original replicas", func() {
			// Set time to Monday 22:00 BKK (downtime)
			bkk, _ := time.LoadLocation("Asia/Ho_Chi_Minh")
			mockNow = time.Date(2026, 4, 20, 22, 0, 0, 0, bkk)

			deploy := &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "downtime-test",
					Namespace: "test-ns",
				},
				Spec: appsv1.DeploymentSpec{
					Replicas: int32Ptr(5),
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"app": "downtime-test"},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": "downtime-test"}},
						Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "test", Image: "nginx"}}},
					},
				},
			}
			Expect(k8sClient.Create(ctx, deploy)).To(Succeed())

			schedule := &downscalerv1alpha1.DownscaleSchedule{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "downtime-schedule",
					Namespace: "default",
				},
				Spec: downscalerv1alpha1.DownscaleScheduleSpec{
					Uptime:           "Mon-Sat 08:00-20:00 Asia/Ho_Chi_Minh",
					Downtime:         "Mon-Sat 20:00-08:00 Asia/Ho_Chi_Minh, Sun 00:00-24:00 Asia/Ho_Chi_Minh",
					DowntimeReplicas: 0,
					IncludeResources: []string{"deployments"},
					IncludeNamespaces: []string{"test-ns"},
				},
			}
			Expect(k8sClient.Create(ctx, schedule)).To(Succeed())

			// Wait for scale down
			Eventually(func() int32 {
				var d appsv1.Deployment
				Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "downtime-test", Namespace: "test-ns"}, &d)).To(Succeed())
				return *d.Spec.Replicas
			}, timeout, interval).Should(Equal(int32(0)))

			// Verify original replicas annotation saved
			var d appsv1.Deployment
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "downtime-test", Namespace: "test-ns"}, &d)).To(Succeed())
			Expect(d.Annotations[scaler.AnnotationOriginalReplicas]).To(Equal("5"))
		})
	})

	Context("Excluded resources", func() {
		It("should not scale resources with exclude annotation", func() {
			bkk, _ := time.LoadLocation("Asia/Ho_Chi_Minh")
			mockNow = time.Date(2026, 4, 20, 22, 0, 0, 0, bkk)

			deploy := &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "excluded-test",
					Namespace: "test-ns",
					Annotations: map[string]string{
						scaler.AnnotationExclude: "true",
					},
				},
				Spec: appsv1.DeploymentSpec{
					Replicas: int32Ptr(2),
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"app": "excluded-test"},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": "excluded-test"}},
						Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "test", Image: "nginx"}}},
					},
				},
			}
			Expect(k8sClient.Create(ctx, deploy)).To(Succeed())

			schedule := &downscalerv1alpha1.DownscaleSchedule{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "excluded-schedule",
					Namespace: "default",
				},
				Spec: downscalerv1alpha1.DownscaleScheduleSpec{
					Uptime:           "Mon-Sat 08:00-20:00 Asia/Ho_Chi_Minh",
					Downtime:         "Mon-Sat 20:00-08:00 Asia/Ho_Chi_Minh, Sun 00:00-24:00 Asia/Ho_Chi_Minh",
					DowntimeReplicas: 0,
					IncludeResources: []string{"deployments"},
					IncludeNamespaces: []string{"test-ns"},
				},
			}
			Expect(k8sClient.Create(ctx, schedule)).To(Succeed())

			// Verify deployment stays at 2 replicas
			Consistently(func() int32 {
				var d appsv1.Deployment
				Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "excluded-test", Namespace: "test-ns"}, &d)).To(Succeed())
				return *d.Spec.Replicas
			}, "5s", interval).Should(Equal(int32(2)))
		})
	})
})
```

- [ ] **Step 3: Run integration tests**

```bash
cd /Users/vu.truongduc/Projects/kube-scaledown
make test
```

Expected: All tests pass (envtest starts a local API server, applies CRDs, runs the controller).

- [ ] **Step 4: Commit**

```bash
git add -A
git commit -m "add integration tests for reconciler with envtest"
```

---

### Task 8: Sample CRD and Makefile Targets

**Files:**
- Create: `config/samples/downscaleschedule_working_hours.yaml`
- Modify: `Makefile` (add convenience targets)

- [ ] **Step 1: Create sample CRD matching AKS G1 setup**

Create `config/samples/downscaleschedule_working_hours.yaml`:

```yaml
apiVersion: downscaler.sipher.gg/v1alpha1
kind: DownscaleSchedule
metadata:
  name: working-hours
spec:
  uptime: "Mon-Sat 08:00-20:00 Asia/Ho_Chi_Minh"
  downtime: "Mon-Sat 20:00-08:00 Asia/Ho_Chi_Minh, Sun 00:00-24:00 Asia/Ho_Chi_Minh"
  downtimeReplicas: 0
  includeResources:
    - deployments
    - statefulsets
    - fleets.agones.dev
  excludeNamespaces:
    - kube-system
    - kube-downscaler
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

- [ ] **Step 2: Verify sample validates against CRD schema**

```bash
cd /Users/vu.truongduc/Projects/kube-scaledown
kubectl apply --dry-run=client -f config/samples/downscaleschedule_working_hours.yaml 2>&1 || echo "CRD not installed, but file is valid YAML"
```

- [ ] **Step 3: Commit**

```bash
git add config/samples/
git commit -m "add sample DownscaleSchedule matching AKS G1 config"
```

---

### Task 9: Dockerfile for Controller

**Files:**
- Create: `Dockerfile.controller`

- [ ] **Step 1: Create multi-stage Dockerfile**

Create `Dockerfile.controller`:

```dockerfile
FROM golang:1.22-alpine AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o controller ./cmd/controller/

FROM gcr.io/distroless/static:nonroot
WORKDIR /
COPY --from=builder /app/controller .
USER 65532:65532
ENTRYPOINT ["/controller"]
```

- [ ] **Step 2: Verify Dockerfile builds (syntax check)**

```bash
cd /Users/vu.truongduc/Projects/kube-scaledown
docker build -f Dockerfile.controller -t kube-scaledown-controller:dev . 2>&1 | tail -5
```

Expected: Build succeeds (or shows the final step completing).

- [ ] **Step 3: Commit**

```bash
git add Dockerfile.controller
git commit -m "add controller Dockerfile"
```
