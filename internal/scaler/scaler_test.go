package scaler

import (
	"context"
	"errors"
	"reflect"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func newDeployment(name string, replicas int32, annotations map[string]string) *appsv1.Deployment {
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Namespace:   "default",
			Annotations: annotations,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: new(replicas),
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
	deploy := newDeployment("test", 3, map[string]string{
		AnnotationOriginalReplicas: "3",
		AnnotationOwner:            "default/nightly",
	})

	ClearOriginalReplicas(deploy)
	if _, ok := deploy.GetAnnotations()[AnnotationOriginalReplicas]; ok {
		t.Error("expected annotation to be cleared")
	}
	if _, ok := deploy.GetAnnotations()[AnnotationOwner]; ok {
		t.Error("expected owner annotation to be cleared")
	}
}

func TestScaleUpOwnedOnlyRestoresMatchingOwner(t *testing.T) {
	ctx := context.Background()
	deployment := newDeployment("test", 0, map[string]string{
		AnnotationOriginalReplicas: "4",
		AnnotationOwner:            "default/preview-downtime",
	})
	c := fake.NewClientBuilder().WithObjects(deployment).Build()
	s := &DeploymentScaler{}

	scaled, err := ScaleUpOwned(ctx, c, s, deployment, "default/broad-uptime")
	if err != nil {
		t.Fatalf("ScaleUpOwned() for another owner error = %v", err)
	}
	if scaled {
		t.Fatal("ScaleUpOwned() for another owner = true, want false")
	}
	stored := newDeployment("test", 0, nil)
	if err := c.Get(ctx, client.ObjectKeyFromObject(deployment), stored); err != nil {
		t.Fatal(err)
	}
	if *stored.Spec.Replicas != 0 || stored.Annotations[AnnotationOwner] != "default/preview-downtime" {
		t.Fatalf("another owner changed resource: replicas=%d annotations=%v", *stored.Spec.Replicas, stored.Annotations)
	}

	scaled, err = ScaleUpOwned(ctx, c, s, stored, "default/preview-downtime")
	if err != nil {
		t.Fatalf("ScaleUpOwned() for matching owner error = %v", err)
	}
	if !scaled {
		t.Fatal("ScaleUpOwned() for matching owner = false, want true")
	}
	if err := c.Get(ctx, client.ObjectKeyFromObject(deployment), stored); err != nil {
		t.Fatal(err)
	}
	if *stored.Spec.Replicas != 4 {
		t.Fatalf("restored replicas = %d, want 4", *stored.Spec.Replicas)
	}
	if _, ok := stored.Annotations[AnnotationOwner]; ok {
		t.Fatal("owner annotation was not removed after restore")
	}
}

func TestScaleUpOwnedRestoresLegacyOwnerlessState(t *testing.T) {
	ctx := context.Background()
	deployment := newDeployment("legacy", 0, map[string]string{LegacyAnnotationOriginalReplicas: "2"})
	c := fake.NewClientBuilder().WithObjects(deployment).Build()

	scaled, err := ScaleUpOwned(ctx, c, &DeploymentScaler{}, deployment, "default/nightly")
	if err != nil {
		t.Fatalf("ScaleUpOwned() error = %v", err)
	}
	if !scaled {
		t.Fatal("ScaleUpOwned() = false, want true for legacy ownerless state")
	}
	stored := newDeployment("legacy", 0, nil)
	if err := c.Get(ctx, client.ObjectKeyFromObject(deployment), stored); err != nil {
		t.Fatal(err)
	}
	if *stored.Spec.Replicas != 2 {
		t.Fatalf("restored replicas = %d, want 2", *stored.Spec.Replicas)
	}
	if _, ok := stored.Annotations[LegacyAnnotationOriginalReplicas]; ok {
		t.Fatal("legacy original replicas annotation was not removed")
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

func TestScaleDownAndUpRestoreWorkloads(t *testing.T) {
	tests := []struct {
		name             string
		scaler           Scaler
		object           client.Object
		originalReplicas int32
	}{
		{
			name:             "Deployment",
			scaler:           &DeploymentScaler{},
			object:           newDeployment("deployment", 3, nil),
			originalReplicas: 3,
		},
		{
			name:   "StatefulSet",
			scaler: &StatefulSetScaler{},
			object: &appsv1.StatefulSet{
				ObjectMeta: metav1.ObjectMeta{Name: "statefulset", Namespace: "default"},
				Spec:       appsv1.StatefulSetSpec{Replicas: new(int32(4))},
			},
			originalReplicas: 4,
		},
		{
			name:   "CronJob",
			scaler: &CronJobScaler{},
			object: &batchv1.CronJob{
				ObjectMeta: metav1.ObjectMeta{Name: "cronjob", Namespace: "default"},
			},
			originalReplicas: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			c := fake.NewClientBuilder().WithObjects(tt.object).Build()

			scaled, err := ScaleDown(ctx, c, tt.scaler, tt.object, 0)
			if err != nil {
				t.Fatalf("ScaleDown() error = %v", err)
			}
			if !scaled {
				t.Fatal("ScaleDown() = false, want true")
			}

			stored, err := newObjectOfSameType(tt.object)
			if err != nil {
				t.Fatal(err)
			}
			if err := c.Get(ctx, client.ObjectKeyFromObject(tt.object), stored); err != nil {
				t.Fatal(err)
			}
			if replicas, err := tt.scaler.GetReplicas(stored); err != nil || replicas != 0 {
				t.Fatalf("scaled-down replicas = %d, err = %v, want 0", replicas, err)
			}

			scaled, err = ScaleUp(ctx, c, tt.scaler, stored)
			if err != nil {
				t.Fatalf("ScaleUp() error = %v", err)
			}
			if !scaled {
				t.Fatal("ScaleUp() = false, want true")
			}

			restored, err := newObjectOfSameType(tt.object)
			if err != nil {
				t.Fatal(err)
			}
			if err := c.Get(ctx, client.ObjectKeyFromObject(tt.object), restored); err != nil {
				t.Fatal(err)
			}
			if replicas, err := tt.scaler.GetReplicas(restored); err != nil || replicas != tt.originalReplicas {
				t.Fatalf("restored replicas = %d, err = %v, want %d", replicas, err, tt.originalReplicas)
			}
			if _, ok := restored.GetAnnotations()[AnnotationOriginalReplicas]; ok {
				t.Fatal("original replicas annotation was not removed")
			}
		})
	}
}

func TestFleetScaleDownAndUpRestoresAutoscalerAfterConflicts(t *testing.T) {
	ctx := context.Background()
	fleet := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "agones.dev/v1",
		"kind":       "Fleet",
		"metadata": map[string]any{
			"name":      "game",
			"namespace": "default",
		},
		"spec": map[string]any{"replicas": int64(5)},
	}}
	fleet.SetGroupVersionKind(schema.GroupVersionKind{Group: "agones.dev", Version: "v1", Kind: "Fleet"})
	fleetAutoscaler := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "autoscaling.agones.dev/v1",
		"kind":       "FleetAutoscaler",
		"metadata": map[string]any{
			"name":      "autoscaler-game",
			"namespace": "default",
		},
		"spec": map[string]any{
			"fleetName": "game",
			"policy": map[string]any{
				"type": "Buffer",
				"buffer": map[string]any{
					"bufferSize":  "20%",
					"minReplicas": int64(2),
					"maxReplicas": int64(20),
				},
			},
		},
	}}
	fleetAutoscaler.SetGroupVersionKind(fleetAutoscalerGVK)
	wantSpec := fleetAutoscaler.Object["spec"]

	baseClient := fake.NewClientBuilder().WithObjects(fleet, fleetAutoscaler).Build()
	conflictingClient := &conflictClient{Client: baseClient, remainingUpdateConflicts: 1}
	s := &FleetScaler{}

	scaled, err := ScaleDown(ctx, conflictingClient, s, fleet, 0)
	if err != nil {
		t.Fatalf("ScaleDown() error = %v", err)
	}
	if !scaled {
		t.Fatal("ScaleDown() = false, want true")
	}
	if err := baseClient.Get(ctx, client.ObjectKeyFromObject(fleetAutoscaler), &unstructured.Unstructured{Object: map[string]any{"apiVersion": "autoscaling.agones.dev/v1", "kind": "FleetAutoscaler"}}); !apierrors.IsNotFound(err) {
		t.Fatalf("FleetAutoscaler still exists after scale down: %v", err)
	}

	storedFleet := &unstructured.Unstructured{}
	storedFleet.SetGroupVersionKind(fleet.GroupVersionKind())
	if err := baseClient.Get(ctx, client.ObjectKeyFromObject(fleet), storedFleet); err != nil {
		t.Fatal(err)
	}
	if _, ok := storedFleet.GetAnnotations()[annotationFASSpec]; !ok {
		t.Fatal("FleetAutoscaler spec annotation was lost after update conflict")
	}

	conflictingClient.remainingUpdateConflicts = 1
	scaled, err = ScaleUp(ctx, conflictingClient, s, storedFleet)
	if err != nil {
		t.Fatalf("ScaleUp() error = %v", err)
	}
	if !scaled {
		t.Fatal("ScaleUp() = false, want true")
	}

	restoredFleet := &unstructured.Unstructured{}
	restoredFleet.SetGroupVersionKind(fleet.GroupVersionKind())
	if err := baseClient.Get(ctx, client.ObjectKeyFromObject(fleet), restoredFleet); err != nil {
		t.Fatal(err)
	}
	if replicas, err := s.GetReplicas(restoredFleet); err != nil || replicas != 5 {
		t.Fatalf("restored Fleet replicas = %d, err = %v, want 5", replicas, err)
	}
	if _, ok := restoredFleet.GetAnnotations()[annotationFASSpec]; ok {
		t.Fatal("FleetAutoscaler spec annotation was not removed")
	}

	restoredAutoscaler := &unstructured.Unstructured{}
	restoredAutoscaler.SetGroupVersionKind(fleetAutoscalerGVK)
	if err := baseClient.Get(ctx, client.ObjectKeyFromObject(fleetAutoscaler), restoredAutoscaler); err != nil {
		t.Fatalf("getting restored FleetAutoscaler: %v", err)
	}
	if !reflect.DeepEqual(restoredAutoscaler.Object["spec"], wantSpec) {
		t.Fatalf("restored FleetAutoscaler spec = %#v, want %#v", restoredAutoscaler.Object["spec"], wantSpec)
	}
}

type conflictClient struct {
	client.Client
	remainingUpdateConflicts int
}

func (c *conflictClient) Update(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error {
	if c.remainingUpdateConflicts > 0 {
		c.remainingUpdateConflicts--
		return apierrors.NewConflict(
			schema.GroupResource{Group: obj.GetObjectKind().GroupVersionKind().Group, Resource: "workloads"},
			obj.GetName(),
			errors.New("simulated update conflict"),
		)
	}
	return c.Client.Update(ctx, obj, opts...)
}
