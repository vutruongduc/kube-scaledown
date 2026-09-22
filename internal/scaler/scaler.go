package scaler

import (
	"context"
	"fmt"
	"maps"
	"reflect"
	"strconv"

	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	AnnotationOriginalReplicas       = "downscaler.sipher.gg/original-replicas"
	LegacyAnnotationOriginalReplicas = "downscaler/original-replicas"
	AnnotationOwner                  = "downscaler.sipher.gg/owner"
	AnnotationExclude                = "downscaler.sipher.gg/exclude"
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

// All returns every registered scaler.
func (r *Registry) All() []Scaler {
	result := make([]Scaler, 0, len(r.scalers))
	for _, s := range r.scalers {
		result = append(result, s)
	}
	return result
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
	saveOriginalReplicas(obj, replicas, "")
}

func saveOriginalReplicas(obj client.Object, replicas int32, owner string) {
	annotations := obj.GetAnnotations()
	if annotations == nil {
		annotations = make(map[string]string)
	}
	annotations[AnnotationOriginalReplicas] = strconv.Itoa(int(replicas))
	if owner != "" {
		annotations[AnnotationOwner] = owner
	}
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
		val, ok = annotations[LegacyAnnotationOriginalReplicas]
	}
	if !ok {
		return 1
	}
	n, err := strconv.ParseInt(val, 10, 32)
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
	delete(annotations, LegacyAnnotationOriginalReplicas)
	delete(annotations, AnnotationOwner)
	obj.SetAnnotations(annotations)
}

// IsOwnedBy reports whether owner may restore a scaled-down resource. Resources
// without an owner annotation predate schedule ownership and remain restorable.
func IsOwnedBy(obj client.Object, owner string) bool {
	annotations := obj.GetAnnotations()
	if annotations == nil {
		return false
	}
	currentOwner, hasOwner := annotations[AnnotationOwner]
	return !hasOwner || currentOwner == owner
}

// SideEffectScaler can apply side effects before scaling operations.
// BeforeScaleDown is called before the resource is scaled down (may mutate obj annotations).
// BeforeScaleUp is called before the resource is scaled up (may mutate obj annotations).
type SideEffectScaler interface {
	BeforeScaleDown(ctx context.Context, c client.Client, obj client.Object) error
	BeforeScaleUp(ctx context.Context, c client.Client, obj client.Object) error
}

// ScaleDown scales a resource down and saves original replicas. Returns true if scaled.
func ScaleDown(ctx context.Context, c client.Client, s Scaler, obj client.Object, downtimeReplicas int32) (bool, error) {
	return ScaleDownOwned(ctx, c, s, obj, downtimeReplicas, "")
}

// ScaleDownOwned scales a resource down and records the schedule that owns the
// saved state. An existing state owned by another schedule is left unchanged.
func ScaleDownOwned(ctx context.Context, c client.Client, s Scaler, obj client.Object, downtimeReplicas int32, owner string) (bool, error) {
	key := client.ObjectKeyFromObject(obj)
	preservedAnnotations := make(map[string]string)
	scaled := false

	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		currentObj, err := newObjectOfSameType(obj)
		if err != nil {
			return err
		}
		if err := c.Get(ctx, key, currentObj); err != nil {
			return err
		}
		mergeAnnotations(currentObj, preservedAnnotations)
		if hasOriginalReplicas(currentObj) && !IsOwnedBy(currentObj, owner) {
			return nil
		}

		current, err := s.GetReplicas(currentObj)
		if err != nil {
			return err
		}
		if current <= downtimeReplicas {
			if len(preservedAnnotations) > 0 {
				return c.Update(ctx, currentObj)
			}
			return nil
		}

		beforeAnnotations := copyAnnotations(currentObj.GetAnnotations())
		if ses, ok := s.(SideEffectScaler); ok {
			if err := ses.BeforeScaleDown(ctx, c, currentObj); err != nil {
				return fmt.Errorf("pre-scaledown side effects for %s/%s: %w", key.Namespace, key.Name, err)
			}
		}
		saveOriginalReplicas(currentObj, current, owner)
		rememberChangedAnnotations(preservedAnnotations, beforeAnnotations, currentObj.GetAnnotations())
		if err := s.SetReplicas(currentObj, downtimeReplicas); err != nil {
			return err
		}
		if err := c.Update(ctx, currentObj); err != nil {
			return err
		}
		scaled = true
		return nil
	})
	if err != nil {
		return false, fmt.Errorf("updating %s/%s: %w", key.Namespace, key.Name, err)
	}
	return scaled, nil
}

// ScaleUp restores a resource to its original replicas. Returns true if scaled.
func ScaleUp(ctx context.Context, c client.Client, s Scaler, obj client.Object) (bool, error) {
	return ScaleUpOwned(ctx, c, s, obj, "")
}

// ScaleUpOwned restores a resource only when it is owned by this schedule.
// Ownerless annotations from older controller versions remain restorable.
func ScaleUpOwned(ctx context.Context, c client.Client, s Scaler, obj client.Object, owner string) (bool, error) {
	key := client.ObjectKeyFromObject(obj)
	scaled := false

	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		currentObj, err := newObjectOfSameType(obj)
		if err != nil {
			return err
		}
		if err := c.Get(ctx, key, currentObj); err != nil {
			return err
		}
		if !IsOwnedBy(currentObj, owner) {
			return nil
		}

		original := GetOriginalReplicas(currentObj)
		current, err := s.GetReplicas(currentObj)
		if err != nil {
			return err
		}
		if ses, ok := s.(SideEffectScaler); ok {
			if err := ses.BeforeScaleUp(ctx, c, currentObj); err != nil {
				return fmt.Errorf("pre-scaleup side effects for %s/%s: %w", key.Namespace, key.Name, err)
			}
		}
		ClearOriginalReplicas(currentObj)
		if current < original {
			if err := s.SetReplicas(currentObj, original); err != nil {
				return err
			}
			scaled = true
		}
		return c.Update(ctx, currentObj)
	})
	if err != nil {
		return false, fmt.Errorf("updating %s/%s: %w", key.Namespace, key.Name, err)
	}
	return scaled, nil
}

func hasOriginalReplicas(obj client.Object) bool {
	annotations := obj.GetAnnotations()
	if annotations == nil {
		return false
	}
	if _, ok := annotations[AnnotationOriginalReplicas]; ok {
		return true
	}
	_, ok := annotations[LegacyAnnotationOriginalReplicas]
	return ok
}

func newObjectOfSameType(obj client.Object) (client.Object, error) {
	t := reflect.TypeOf(obj)
	if t == nil || t.Kind() != reflect.Ptr {
		return nil, fmt.Errorf("expected pointer Kubernetes object, got %T", obj)
	}
	fresh, ok := reflect.New(t.Elem()).Interface().(client.Object)
	if !ok {
		return nil, fmt.Errorf("expected Kubernetes object, got %T", obj)
	}
	if obj.GetObjectKind().GroupVersionKind() != (schema.GroupVersionKind{}) {
		fresh.GetObjectKind().SetGroupVersionKind(obj.GetObjectKind().GroupVersionKind())
	}
	return fresh, nil
}

func copyAnnotations(annotations map[string]string) map[string]string {
	result := make(map[string]string, len(annotations))
	maps.Copy(result, annotations)
	return result
}

func rememberChangedAnnotations(preserved, before, after map[string]string) {
	for key, value := range after {
		if previous, ok := before[key]; !ok || previous != value {
			preserved[key] = value
		}
	}
}

func mergeAnnotations(obj client.Object, additions map[string]string) {
	if len(additions) == 0 {
		return
	}
	annotations := copyAnnotations(obj.GetAnnotations())
	maps.Copy(annotations, additions)
	obj.SetAnnotations(annotations)
}
