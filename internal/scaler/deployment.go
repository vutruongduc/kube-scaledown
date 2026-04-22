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
