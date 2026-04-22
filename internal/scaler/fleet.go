package scaler

import (
	"context"
	"encoding/json"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const annotationFASSpec = "downscaler.sipher.gg/fas-spec"

var fleetAutoscalerGVK = schema.GroupVersionKind{
	Group:   "autoscaling.agones.dev",
	Version: "v1",
	Kind:    "FleetAutoscaler",
}

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
	list.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "agones.dev",
		Version: "v1",
		Kind:    "FleetList",
	})
	return list
}

// BeforeScaleDown saves the FleetAutoscaler spec as an annotation on the Fleet and deletes the FAS.
// This prevents the FAS from fighting the scaledown by maintaining minReplicas.
func (f *FleetScaler) BeforeScaleDown(ctx context.Context, c client.Client, obj client.Object) error {
	fasName := "autoscaler-" + obj.GetName()
	fas := &unstructured.Unstructured{}
	fas.SetGroupVersionKind(fleetAutoscalerGVK)

	err := c.Get(ctx, types.NamespacedName{Name: fasName, Namespace: obj.GetNamespace()}, fas)
	if apierrors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("getting FleetAutoscaler %s/%s: %w", obj.GetNamespace(), fasName, err)
	}

	specJSON, err := json.Marshal(fas.Object["spec"])
	if err != nil {
		return fmt.Errorf("marshaling FleetAutoscaler spec: %w", err)
	}

	annotations := obj.GetAnnotations()
	if annotations == nil {
		annotations = make(map[string]string)
	}
	annotations[annotationFASSpec] = string(specJSON)
	obj.SetAnnotations(annotations)

	return c.Delete(ctx, fas)
}

// BeforeScaleUp recreates the FleetAutoscaler from the saved spec annotation.
// The annotation is removed from obj in-memory; ScaleUp's c.Update persists the removal.
func (f *FleetScaler) BeforeScaleUp(ctx context.Context, c client.Client, obj client.Object) error {
	annotations := obj.GetAnnotations()
	if annotations == nil {
		return nil
	}
	specJSON, ok := annotations[annotationFASSpec]
	if !ok {
		return nil
	}

	var spec map[string]interface{}
	if err := json.Unmarshal([]byte(specJSON), &spec); err != nil {
		return fmt.Errorf("unmarshaling FleetAutoscaler spec: %w", err)
	}

	fas := &unstructured.Unstructured{}
	fas.SetGroupVersionKind(fleetAutoscalerGVK)
	fas.SetName("autoscaler-" + obj.GetName())
	fas.SetNamespace(obj.GetNamespace())
	fas.Object["spec"] = spec

	if err := c.Create(ctx, fas); err != nil && !apierrors.IsAlreadyExists(err) {
		return fmt.Errorf("recreating FleetAutoscaler %s/%s: %w", obj.GetNamespace(), fas.GetName(), err)
	}

	delete(annotations, annotationFASSpec)
	obj.SetAnnotations(annotations)

	return nil
}
