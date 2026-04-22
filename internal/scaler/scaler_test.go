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
