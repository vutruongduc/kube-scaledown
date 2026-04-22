package controller

import (
	"context"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	downscalerv1alpha1 "github.com/sipherxyz/kube-scaledown/api/v1alpha1"
	"github.com/sipherxyz/kube-scaledown/internal/scaler"
	"github.com/sipherxyz/kube-scaledown/internal/schedule"
)

type DownscaleScheduleReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Registry *scaler.Registry
	Now      func() time.Time // injectable for testing
}

// +kubebuilder:rbac:groups=downscaler.sipher.gg,resources=downscaleschedules,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=downscaler.sipher.gg,resources=downscaleschedules/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=apps,resources=statefulsets,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=batch,resources=cronjobs,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=agones.dev,resources=fleets;fleets/scale,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch

func (r *DownscaleScheduleReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	var ds downscalerv1alpha1.DownscaleSchedule
	if err := r.Get(ctx, req.NamespacedName, &ds); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	uptimeRules, err := schedule.Parse(ds.Spec.Uptime)
	if err != nil {
		logger.Error(err, "failed to parse uptime schedule")
		return ctrl.Result{RequeueAfter: 60 * time.Second}, nil
	}

	now := r.now()
	isUptime := schedule.IsActive(uptimeRules, now)
	state := "downtime"
	if isUptime {
		state = "uptime"
	}

	var managedCount, scaledDownCount int32

	for _, resourceName := range ds.Spec.IncludeResources {
		s, err := r.Registry.Get(resourceName)
		if err != nil {
			logger.Error(err, "unknown resource type", "resource", resourceName)
			continue
		}

		objects, err := r.listResources(ctx, s, &ds)
		if err != nil {
			logger.Error(err, "failed to list resources", "resource", resourceName)
			continue
		}

		for _, obj := range objects {
			if scaler.IsExcluded(obj) {
				continue
			}
			if r.isResourceExcluded(obj, ds.Spec.ExcludeResources) {
				continue
			}

			managedCount++

			if isUptime {
				if hasOriginalReplicasAnnotation(obj) {
					scaled, err := scaler.ScaleUp(ctx, r.Client, s, obj)
					if err != nil {
						logger.Error(err, "failed to scale up", "resource", obj.GetName(), "namespace", obj.GetNamespace())
						continue
					}
					if scaled {
						logger.Info("scaled up", "resource", obj.GetName(), "namespace", obj.GetNamespace())
					}
				}
			} else {
				scaled, err := scaler.ScaleDown(ctx, r.Client, s, obj, ds.Spec.DowntimeReplicas)
				if err != nil {
					logger.Error(err, "failed to scale down", "resource", obj.GetName(), "namespace", obj.GetNamespace())
					continue
				}
				if scaled {
					logger.Info("scaled down", "resource", obj.GetName(), "namespace", obj.GetNamespace())
				}
				currentReplicas, _ := s.GetReplicas(obj)
				if currentReplicas <= ds.Spec.DowntimeReplicas {
					scaledDownCount++
				}
			}
		}
	}

	ds.Status.CurrentState = state
	ds.Status.ManagedResources = managedCount
	ds.Status.ScaledDownResources = scaledDownCount
	nowMeta := metav1.NewTime(now)
	if state == "downtime" {
		ds.Status.LastScaleDown = &nowMeta
	} else {
		ds.Status.LastScaleUp = &nowMeta
	}
	if err := r.Status().Update(ctx, &ds); err != nil {
		logger.Error(err, "failed to update status")
	}

	return ctrl.Result{RequeueAfter: 60 * time.Second}, nil
}

func (r *DownscaleScheduleReconciler) now() time.Time {
	if r.Now != nil {
		return r.Now()
	}
	return time.Now()
}

func (r *DownscaleScheduleReconciler) listResources(ctx context.Context, s scaler.Scaler, ds *downscalerv1alpha1.DownscaleSchedule) ([]client.Object, error) {
	list := s.NewObjectList()

	namespaces := ds.Spec.IncludeNamespaces
	if len(namespaces) == 0 {
		if err := r.List(ctx, list); err != nil {
			return nil, err
		}
		return r.filterByNamespace(extractObjects(list), ds.Spec.ExcludeNamespaces), nil
	}

	var result []client.Object
	for _, ns := range namespaces {
		if err := r.List(ctx, list, client.InNamespace(ns)); err != nil {
			return nil, err
		}
		result = append(result, extractObjects(list)...)
	}
	return result, nil
}

func extractObjects(list client.ObjectList) []client.Object {
	switch l := list.(type) {
	case *appsv1.DeploymentList:
		objs := make([]client.Object, len(l.Items))
		for i := range l.Items {
			objs[i] = &l.Items[i]
		}
		return objs
	case *appsv1.StatefulSetList:
		objs := make([]client.Object, len(l.Items))
		for i := range l.Items {
			objs[i] = &l.Items[i]
		}
		return objs
	case *batchv1.CronJobList:
		objs := make([]client.Object, len(l.Items))
		for i := range l.Items {
			objs[i] = &l.Items[i]
		}
		return objs
	case *unstructured.UnstructuredList:
		objs := make([]client.Object, len(l.Items))
		for i := range l.Items {
			objs[i] = &l.Items[i]
		}
		return objs
	default:
		return nil
	}
}

func (r *DownscaleScheduleReconciler) filterByNamespace(objs []client.Object, excludeNamespaces []string) []client.Object {
	if len(excludeNamespaces) == 0 {
		return objs
	}
	excludeSet := make(map[string]bool, len(excludeNamespaces))
	for _, ns := range excludeNamespaces {
		excludeSet[ns] = true
	}
	var filtered []client.Object
	for _, obj := range objs {
		if !excludeSet[obj.GetNamespace()] {
			filtered = append(filtered, obj)
		}
	}
	return filtered
}

func (r *DownscaleScheduleReconciler) isResourceExcluded(obj client.Object, excludes []downscalerv1alpha1.ResourceRef) bool {
	for _, ref := range excludes {
		if ref.Name == obj.GetName() && ref.Namespace == obj.GetNamespace() {
			return true
		}
	}
	return false
}

func hasOriginalReplicasAnnotation(obj client.Object) bool {
	annotations := obj.GetAnnotations()
	if annotations == nil {
		return false
	}
	_, ok := annotations[scaler.AnnotationOriginalReplicas]
	return ok
}

func (r *DownscaleScheduleReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&downscalerv1alpha1.DownscaleSchedule{}).
		Complete(r)
}
