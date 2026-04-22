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

// SideEffectScaler can apply side effects before scaling operations.
// BeforeScaleDown is called before the resource is scaled down (may mutate obj annotations).
// BeforeScaleUp is called before the resource is scaled up (may mutate obj annotations).
type SideEffectScaler interface {
	BeforeScaleDown(ctx context.Context, c client.Client, obj client.Object) error
	BeforeScaleUp(ctx context.Context, c client.Client, obj client.Object) error
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
	if ses, ok := s.(SideEffectScaler); ok {
		if err := ses.BeforeScaleDown(ctx, c, obj); err != nil {
			return false, fmt.Errorf("pre-scaledown side effects for %s/%s: %w", obj.GetNamespace(), obj.GetName(), err)
		}
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
	if ses, ok := s.(SideEffectScaler); ok {
		if err := ses.BeforeScaleUp(ctx, c, obj); err != nil {
			return false, fmt.Errorf("pre-scaleup side effects for %s/%s: %w", obj.GetNamespace(), obj.GetName(), err)
		}
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
