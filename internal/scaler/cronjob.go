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
