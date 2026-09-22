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
	obj.SetAnnotations(annotations)
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
		SaveOriginalReplicas(currentObj, current)
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
