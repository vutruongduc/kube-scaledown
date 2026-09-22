package controller

import (
	"context"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	downscalerv1alpha1 "github.com/sipherxyz/kube-scaledown/api/v1alpha1"
	"github.com/sipherxyz/kube-scaledown/internal/scaler"
)

func TestReconcileOnlyScalesIncludedNamespaces(t *testing.T) {
	ctx := context.Background()
	scheme := testScheme(t)
	schedule := testSchedule("included-namespaces")
	schedule.Spec.IncludeNamespaces = []string{"included"}
	included := testDeployment("included", "included", 3)
	notIncluded := testDeployment("not-included", "other", 4)
	c := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&downscalerv1alpha1.DownscaleSchedule{}).
		WithObjects(schedule, included, notIncluded).
		Build()
	reconciler := &DownscaleScheduleReconciler{
		Client:   c,
		Scheme:   scheme,
		Registry: scaler.NewRegistry(),
		Now:      func() time.Time { return scheduleTime(t, 22) },
	}

	if _, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(schedule)}); err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}

	assertDeploymentReplicas(t, ctx, c, client.ObjectKeyFromObject(included), 0)
	assertDeploymentReplicas(t, ctx, c, client.ObjectKeyFromObject(notIncluded), 4)
	status := getSchedule(t, ctx, c, client.ObjectKeyFromObject(schedule)).Status
	if status.ManagedResources != 1 || status.ScaledDownResources != 1 {
		t.Fatalf("first downtime status = managed %d, scaled down %d; want 1, 1", status.ManagedResources, status.ScaledDownResources)
	}
}

func TestReconcileRecordsOnlyStateTransitionTimes(t *testing.T) {
	ctx := context.Background()
	scheme := testScheme(t)
	schedule := testSchedule("transition-times")
	c := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&downscalerv1alpha1.DownscaleSchedule{}).
		WithObjects(schedule).
		Build()
	currentTime := scheduleTime(t, 10)
	reconciler := &DownscaleScheduleReconciler{
		Client:   c,
		Scheme:   scheme,
		Registry: scaler.NewRegistry(),
		Now:      func() time.Time { return currentTime },
	}
	request := ctrl.Request{NamespacedName: client.ObjectKeyFromObject(schedule)}

	if _, err := reconciler.Reconcile(ctx, request); err != nil {
		t.Fatalf("first Reconcile() error = %v", err)
	}
	first := getSchedule(t, ctx, c, request.NamespacedName)
	if first.Status.LastScaleUp == nil || !first.Status.LastScaleUp.Time.Equal(currentTime) {
		t.Fatalf("LastScaleUp = %v, want %v", first.Status.LastScaleUp, currentTime)
	}

	currentTime = scheduleTime(t, 11)
	if _, err := reconciler.Reconcile(ctx, request); err != nil {
		t.Fatalf("second Reconcile() error = %v", err)
	}
	second := getSchedule(t, ctx, c, request.NamespacedName)
	if second.Status.LastScaleUp == nil || !second.Status.LastScaleUp.Equal(first.Status.LastScaleUp) {
		t.Fatalf("LastScaleUp changed without a state transition: first=%v second=%v", first.Status.LastScaleUp, second.Status.LastScaleUp)
	}

	currentTime = scheduleTime(t, 22)
	if _, err := reconciler.Reconcile(ctx, request); err != nil {
		t.Fatalf("third Reconcile() error = %v", err)
	}
	third := getSchedule(t, ctx, c, request.NamespacedName)
	if third.Status.LastScaleDown == nil || !third.Status.LastScaleDown.Time.Equal(currentTime) {
		t.Fatalf("LastScaleDown = %v, want %v", third.Status.LastScaleDown, currentTime)
	}
	if third.Status.LastScaleUp == nil || !third.Status.LastScaleUp.Equal(first.Status.LastScaleUp) {
		t.Fatalf("LastScaleUp changed during scale down: first=%v third=%v", first.Status.LastScaleUp, third.Status.LastScaleUp)
	}
}

func testScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := appsv1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if err := downscalerv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	return scheme
}

func testSchedule(name string) *downscalerv1alpha1.DownscaleSchedule {
	return &downscalerv1alpha1.DownscaleSchedule{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
		Spec: downscalerv1alpha1.DownscaleScheduleSpec{
			Uptime:           "Mon-Fri 08:00-20:00 Asia/Ho_Chi_Minh",
			DowntimeReplicas: 0,
			IncludeResources: []string{"deployments"},
		},
	}
}

func testDeployment(name, namespace string, replicas int32) *appsv1.Deployment {
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec:       appsv1.DeploymentSpec{Replicas: &replicas},
	}
}

func scheduleTime(t *testing.T, hour int) time.Time {
	t.Helper()
	location, err := time.LoadLocation("Asia/Ho_Chi_Minh")
	if err != nil {
		t.Fatal(err)
	}
	return time.Date(2026, 4, 20, hour, 0, 0, 0, location)
}

func assertDeploymentReplicas(t *testing.T, ctx context.Context, c client.Client, key types.NamespacedName, want int32) {
	t.Helper()
	var deployment appsv1.Deployment
	if err := c.Get(ctx, key, &deployment); err != nil {
		t.Fatal(err)
	}
	if deployment.Spec.Replicas == nil || *deployment.Spec.Replicas != want {
		t.Fatalf("Deployment %s replicas = %v, want %d", key, deployment.Spec.Replicas, want)
	}
}

func getSchedule(t *testing.T, ctx context.Context, c client.Client, key types.NamespacedName) *downscalerv1alpha1.DownscaleSchedule {
	t.Helper()
	var schedule downscalerv1alpha1.DownscaleSchedule
	if err := c.Get(ctx, key, &schedule); err != nil {
		t.Fatal(err)
	}
	return &schedule
}
