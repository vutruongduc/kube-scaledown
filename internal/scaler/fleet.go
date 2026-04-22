package scaler

import (
	"fmt"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
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
	list.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "agones.dev",
		Version: "v1",
		Kind:    "FleetList",
	})
	return list
}
