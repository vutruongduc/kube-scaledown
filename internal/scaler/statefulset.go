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
