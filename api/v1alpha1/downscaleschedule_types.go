package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// DownscaleScheduleSpec defines the desired schedule configuration.
type DownscaleScheduleSpec struct {
	// Uptime schedule in format "Mon-Fri 08:00-20:00 Asia/Ho_Chi_Minh"
	// +kubebuilder:validation:Required
	Uptime string `json:"uptime"`

	// Downtime schedule in format "Mon-Sat 20:00-08:00 Asia/Ho_Chi_Minh, Sun 00:00-24:00 Asia/Ho_Chi_Minh"
	// +kubebuilder:validation:Required
	Downtime string `json:"downtime"`

	// Replica count during downtime (default: 0)
	// +kubebuilder:default=0
	// +kubebuilder:validation:Minimum=0
	DowntimeReplicas int32 `json:"downtimeReplicas,omitempty"`

	// Resource types to manage (e.g., "deployments", "statefulsets", "fleets.agones.dev")
	// +kubebuilder:validation:MinItems=1
	IncludeResources []string `json:"includeResources"`

	// Namespaces to include (empty = all namespaces)
	// +optional
	IncludeNamespaces []string `json:"includeNamespaces,omitempty"`

	// Namespaces to exclude
	// +optional
	ExcludeNamespaces []string `json:"excludeNamespaces,omitempty"`

	// Individual resources to exclude
	// +optional
	ExcludeResources []ResourceRef `json:"excludeResources,omitempty"`
}

// ResourceRef identifies a specific resource to exclude.
type ResourceRef struct {
	// Resource name
	Name string `json:"name"`
	// Resource namespace
	Namespace string `json:"namespace"`
	// Resource kind (e.g., "Deployment", "Fleet")
	Kind string `json:"kind"`
}

// DownscaleScheduleStatus defines the observed state.
type DownscaleScheduleStatus struct {
	// Last time resources were scaled down
	// +optional
	LastScaleDown *metav1.Time `json:"lastScaleDown,omitempty"`

	// Last time resources were scaled up
	// +optional
	LastScaleUp *metav1.Time `json:"lastScaleUp,omitempty"`

	// Current state: "uptime" or "downtime"
	CurrentState string `json:"currentState,omitempty"`

	// Number of resources managed by this schedule
	ManagedResources int32 `json:"managedResources,omitempty"`

	// Number of resources currently scaled down
	ScaledDownResources int32 `json:"scaledDownResources,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=ds
// +kubebuilder:printcolumn:name="State",type=string,JSONPath=`.status.currentState`
// +kubebuilder:printcolumn:name="Managed",type=integer,JSONPath=`.status.managedResources`
// +kubebuilder:printcolumn:name="Scaled Down",type=integer,JSONPath=`.status.scaledDownResources`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// DownscaleSchedule is the Schema for the downscaleschedules API.
type DownscaleSchedule struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   DownscaleScheduleSpec   `json:"spec,omitempty"`
	Status DownscaleScheduleStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// DownscaleScheduleList contains a list of DownscaleSchedule.
type DownscaleScheduleList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []DownscaleSchedule `json:"items"`
}

func init() {
	SchemeBuilder.Register(&DownscaleSchedule{}, &DownscaleScheduleList{})
}
