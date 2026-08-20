// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: SUSE LLC
// SPDX-FileCopyrightText: The Rancher Desktop Authors

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

// ComposeKind is the Kind string for Compose resources.
const ComposeKind = "Compose"

// ComposeUpRequestKind is the Kind string for ComposeUpRequest resources.
const ComposeUpRequestKind = "ComposeUpRequest"

// Compose status conditions.
const (
	ComposeConditionHasMembers = "HasMembers"
)

// Compose status reasons for the HasMembers condition.
const (
	// ComposeHasMembersReasonFound indicates that the compose project has at
	// least one member.
	ComposeHasMembersReasonFound = "Found"
	// ComposeHasMembersReasonDeleted indicates that the last member of the
	// compose project has been removed; the project may be deleted soon.
	ComposeHasMembersReasonDeleted = "Deleted"
	// ComposeHasMembersReasonCalculating indicates that the controller is
	// still calculating whether the compose project has any members.
	ComposeHasMembersReasonCalculating = "Calculating"
)

// ComposeUpRequest status conditions.
const (
	ComposeUpRequestConditionSettled = "Settled"
)

// ComposeUpRequest status reasons for the Settled condition.
const (
	// ComposeUpRequestSettledReasonRunning indicates that `docker compose up`
	// is still running.
	ComposeUpRequestSettledReasonRunning = "Running"
	// ComposeUpRequestSettledReasonErrored indicates that `docker compose up`
	// failed.
	ComposeUpRequestSettledReasonErrored = "Errored"
	// ComposeUpRequestSettledReasonSucceeded indicates that `docker compose
	// up` succeeded; this object will be reaped.
	ComposeUpRequestSettledReasonSucceeded = "Succeeded"
)

// ComposeMember describes a single member of a compose project, which is a
// container, image, or volume.
type ComposeMember struct {
	// Name is the name of the member object, in the form [kind]/[name], e.g.
	// "Volume/myvolume".
	//
	// +required
	Name string `json:"name"`
	// UID is the UID of the member object.
	//
	// +required
	UID types.UID `json:"uid"`
}

// ComposeStatus defines the observed state of a `docker compose` project.
type ComposeStatus struct {
	// Namespace is the container namespace; refers to a [ContainerNamespace]
	// object in the same Kubernetes namespace.
	//
	// +required
	Namespace string `json:"namespace"`
	// Name is the compose project name.
	//
	// +required
	Name string `json:"name"`
	// WorkingDir is the compose project directory on the host (i.e. relative
	// to where the RDD process runs). May be unset for a Compose object that
	// was automatically created from observed containers, images, or
	// volumes rather than a [ComposeUpRequest].
	//
	// +optional
	WorkingDir string `json:"workingDir,omitempty"`
	// Configs is the list of compose files used to create the project,
	// relative to WorkingDir. It is not guaranteed that this is sufficient to
	// recreate the project. May be unset for a Compose object that was
	// automatically created from observed containers, images, or volumes.
	//
	// +optional
	Configs []string `json:"configs,omitempty"`
	// Members tracks the objects that are part of the compose project. The
	// name is a combination of object kind and name, e.g. "Volume/myvolume".
	// The UID is also tracked.
	//
	// +listType=map
	// +listMapKey=name
	// +optional
	Members []ComposeMember `json:"members,omitempty"`
	// Conditions represent the state of the compose project.
	// Known condition types include:
	//
	// - "HasMembers": The compose project has at least one member.
	//
	// The status of each condition is one of True, False, or Unknown.
	//
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:ac:generate=true
// +kubebuilder:resource:categories="all"
// +kubebuilder:subresource:status
// +kubebuilder:selectablefield:JSONPath=.status.namespace
// +kubebuilder:selectablefield:JSONPath=.status.name
// +kubebuilder:printcolumn:name="Project",type=string,JSONPath=`.status.name`

// Compose is the Schema for the compose API.  Compose objects do not reflect
// actual container engine objects; instead, they reflect `docker compose`
// projects.
//
// Compose objects cannot be created directly by users: the reconciler creates
// (and keeps up to date) a Compose object automatically upon noticing compose
// project references in containers, images, or volumes (via the
// `com.docker.compose.project` label on those resources).  Said resources may
// have been created as a response to [ComposeUpRequest] objects.  metadata.name
// must be the SHA256 hash of `status.namespace` followed by a literal slash and
// lower-cased `status.name`.
type Compose struct {
	metav1.TypeMeta `json:",inline"`

	// Metadata is a standard object metadata
	//
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty,omitzero"`

	// Status defines the observed state of Compose
	//
	// +optional
	Status ComposeStatus `json:"status,omitempty,omitzero"`
}

// +kubebuilder:object:root=true

// ComposeList contains a list of Compose.
type ComposeList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Compose `json:"items"`
}

// ComposeUpRequestSpec defines the identity of the `docker compose` project
// to bring up.
type ComposeUpRequestSpec struct {
	// Namespace is the container namespace; refers to a [ContainerNamespace]
	// object in the same Kubernetes namespace.  Immutable once created.
	//
	// +required
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="spec.namespace is immutable"
	Namespace string `json:"namespace"`
	// Name is the compose project name.  Immutable once created.
	//
	// +required
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="spec.name is immutable"
	Name string `json:"name"`
	// WorkingDir is the compose project directory on the host (i.e. relative
	// to where the RDD process runs).  Used to look up any files needed.
	// Immutable once created.
	//
	// +required
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="spec.workingDir is immutable"
	WorkingDir string `json:"workingDir"`
	// Configs is the list of compose files used to create the project,
	// relative to WorkingDir.  Immutable once created.
	//
	// +optional
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="spec.configs is immutable"
	Configs []string `json:"configs,omitempty"`
}

// ComposeUpRequestStatus defines the observed state of a ComposeUpRequest.
type ComposeUpRequestStatus struct {
	// Conditions represent the state of the request.
	// Known condition types include:
	//
	// - "Settled": `docker compose up` has finished running, successfully or
	//   not.
	//
	// The status of each condition is one of True, False, or Unknown.
	//
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:ac:generate=true
// +kubebuilder:resource:categories="all"
// +kubebuilder:subresource:status
// +kubebuilder:selectablefield:JSONPath=.spec.namespace
// +kubebuilder:selectablefield:JSONPath=.spec.name
// +kubebuilder:metadata:annotations=rdd.rancherdesktop.io/controller=compose

// ComposeUpRequest is the Schema for the composeuprequests API.  Creating a
// ComposeUpRequest triggers `docker compose up` for the named project, and
// causes the corresponding [Compose] object to be created (or updated) to
// reflect this project.  metadata.name must be constructed the same way as for
// a Compose object, based on spec.namespace and spec.name; this is also the
// name of the resulting Compose object.
//
// Once the underlying command has settled (successfully or not), this object is
// expected to be deleted after a short delay.
type ComposeUpRequest struct {
	metav1.TypeMeta `json:",inline"`

	// Metadata is a standard object metadata
	//
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty,omitzero"`

	// Spec defines the identity of the compose project to bring up.
	//
	// +required
	Spec ComposeUpRequestSpec `json:"spec"`

	// Status defines the observed state of ComposeUpRequest
	//
	// +optional
	Status ComposeUpRequestStatus `json:"status,omitempty,omitzero"`
}

// +kubebuilder:object:root=true

// ComposeUpRequestList contains a list of ComposeUpRequest.
type ComposeUpRequestList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ComposeUpRequest `json:"items"`
}

func init() {
	registerTypes(
		&Compose{}, &ComposeList{},
		&ComposeUpRequest{}, &ComposeUpRequestList{},
	)
}
