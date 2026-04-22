package controller

import (
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	downscalerv1alpha1 "github.com/sipherxyz/kube-scaledown/api/v1alpha1"
	"github.com/sipherxyz/kube-scaledown/internal/scaler"
)

func int32Ptr(i int32) *int32 { return &i }

var _ = Describe("DownscaleSchedule Controller", func() {

	const timeout = 10 * time.Second
	const interval = 250 * time.Millisecond

	Context("During uptime", func() {
		It("should not scale down deployments", func() {
			bkk, _ := time.LoadLocation("Asia/Ho_Chi_Minh")
			mockNow = time.Date(2026, 4, 20, 10, 0, 0, 0, bkk)

			deploy := &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "uptime-test",
					Namespace: "test-ns",
				},
				Spec: appsv1.DeploymentSpec{
					Replicas: int32Ptr(3),
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"app": "uptime-test"},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": "uptime-test"}},
						Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "test", Image: "nginx"}}},
					},
				},
			}
			Expect(k8sClient.Create(ctx, deploy)).To(Succeed())

			ds := &downscalerv1alpha1.DownscaleSchedule{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "uptime-schedule",
					Namespace: "default",
				},
				Spec: downscalerv1alpha1.DownscaleScheduleSpec{
					Uptime:            "Mon-Sat 08:00-20:00 Asia/Ho_Chi_Minh",
					Downtime:          "Mon-Sat 20:00-08:00 Asia/Ho_Chi_Minh, Sun 00:00-24:00 Asia/Ho_Chi_Minh",
					DowntimeReplicas:  0,
					IncludeResources:  []string{"deployments"},
					IncludeNamespaces: []string{"test-ns"},
				},
			}
			Expect(k8sClient.Create(ctx, ds)).To(Succeed())

			Consistently(func() int32 {
				var d appsv1.Deployment
				Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "uptime-test", Namespace: "test-ns"}, &d)).To(Succeed())
				if d.Spec.Replicas == nil {
					return 0
				}
				return *d.Spec.Replicas
			}, "5s", interval).Should(Equal(int32(3)))
		})
	})

	Context("During downtime", func() {
		It("should scale down deployments and save original replicas", func() {
			bkk, _ := time.LoadLocation("Asia/Ho_Chi_Minh")
			mockNow = time.Date(2026, 4, 20, 22, 0, 0, 0, bkk) // Monday 22:00 (downtime)

			deploy := &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "downtime-test",
					Namespace: "test-ns",
				},
				Spec: appsv1.DeploymentSpec{
					Replicas: int32Ptr(5),
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"app": "downtime-test"},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": "downtime-test"}},
						Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "test", Image: "nginx"}}},
					},
				},
			}
			Expect(k8sClient.Create(ctx, deploy)).To(Succeed())

			ds := &downscalerv1alpha1.DownscaleSchedule{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "downtime-schedule",
					Namespace: "default",
				},
				Spec: downscalerv1alpha1.DownscaleScheduleSpec{
					Uptime:            "Mon-Sat 08:00-20:00 Asia/Ho_Chi_Minh",
					Downtime:          "Mon-Sat 20:00-08:00 Asia/Ho_Chi_Minh, Sun 00:00-24:00 Asia/Ho_Chi_Minh",
					DowntimeReplicas:  0,
					IncludeResources:  []string{"deployments"},
					IncludeNamespaces: []string{"test-ns"},
				},
			}
			Expect(k8sClient.Create(ctx, ds)).To(Succeed())

			Eventually(func() int32 {
				var d appsv1.Deployment
				Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "downtime-test", Namespace: "test-ns"}, &d)).To(Succeed())
				if d.Spec.Replicas == nil {
					return -1
				}
				return *d.Spec.Replicas
			}, timeout, interval).Should(Equal(int32(0)))

			var d appsv1.Deployment
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "downtime-test", Namespace: "test-ns"}, &d)).To(Succeed())
			Expect(d.Annotations[scaler.AnnotationOriginalReplicas]).To(Equal("5"))
		})
	})

	Context("Excluded resources", func() {
		It("should not scale resources with exclude annotation", func() {
			bkk, _ := time.LoadLocation("Asia/Ho_Chi_Minh")
			mockNow = time.Date(2026, 4, 20, 22, 0, 0, 0, bkk) // Monday 22:00 (downtime)

			deploy := &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "excluded-test",
					Namespace: "test-ns",
					Annotations: map[string]string{
						scaler.AnnotationExclude: "true",
					},
				},
				Spec: appsv1.DeploymentSpec{
					Replicas: int32Ptr(2),
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"app": "excluded-test"},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": "excluded-test"}},
						Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "test", Image: "nginx"}}},
					},
				},
			}
			Expect(k8sClient.Create(ctx, deploy)).To(Succeed())

			ds := &downscalerv1alpha1.DownscaleSchedule{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "excluded-schedule",
					Namespace: "default",
				},
				Spec: downscalerv1alpha1.DownscaleScheduleSpec{
					Uptime:            "Mon-Sat 08:00-20:00 Asia/Ho_Chi_Minh",
					Downtime:          "Mon-Sat 20:00-08:00 Asia/Ho_Chi_Minh, Sun 00:00-24:00 Asia/Ho_Chi_Minh",
					DowntimeReplicas:  0,
					IncludeResources:  []string{"deployments"},
					IncludeNamespaces: []string{"test-ns"},
				},
			}
			Expect(k8sClient.Create(ctx, ds)).To(Succeed())

			Consistently(func() int32 {
				var d appsv1.Deployment
				Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "excluded-test", Namespace: "test-ns"}, &d)).To(Succeed())
				if d.Spec.Replicas == nil {
					return 0
				}
				return *d.Spec.Replicas
			}, "5s", interval).Should(Equal(int32(2)))
		})
	})
})
