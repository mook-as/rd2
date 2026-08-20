// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: SUSE LLC
// SPDX-FileCopyrightText: The Rancher Desktop Authors

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

// +kubebuilder:validation:Enum=created;running;pausing;paused;restarting;removing;exited;dead;unknown

// ContainerStatusValue describes the status of a container.
type ContainerStatusValue string

// Possible values for ContainerStatusValue.
const (
	ContainerStatusCreated    ContainerStatusValue = "created"
	ContainerStatusRunning    ContainerStatusValue = "running"
	ContainerStatusPausing    ContainerStatusValue = "pausing"
	ContainerStatusPaused     ContainerStatusValue = "paused"
	ContainerStatusRestarting ContainerStatusValue = "restarting"
	ContainerStatusRemoving   ContainerStatusValue = "removing"
	ContainerStatusExited     ContainerStatusValue = "exited"
	ContainerStatusDead       ContainerStatusValue = "dead"
	ContainerStatusUnknown    ContainerStatusValue = "unknown"
)

// AnnotationAction requests a one-shot action on a container. The reconciler
// performs the action, records the outcome in status.lastAction, and removes
// the annotation. Setting a new value replaces any pending action — there is
// no queue.
const AnnotationAction = "containers.rancherdesktop.io/action"

// +kubebuilder:validation:Enum=start;stop;pause;unpause;restart

// ContainerAction is the name of an action that can be requested on a
// container via the AnnotationAction annotation.
type ContainerAction string

// Possible values for ContainerAction.
const (
	ContainerActionStart   ContainerAction = "start"
	ContainerActionStop    ContainerAction = "stop"
	ContainerActionPause   ContainerAction = "pause"
	ContainerActionUnpause ContainerAction = "unpause"
	ContainerActionRestart ContainerAction = "restart"
)

// IsValid reports whether a is one of the defined ContainerAction values.
func (a ContainerAction) IsValid() bool {
	switch a {
	case ContainerActionStart, ContainerActionStop,
		ContainerActionPause, ContainerActionUnpause,
		ContainerActionRestart:
		return true
	}
	return false
}

// +kubebuilder:validation:Enum=Succeeded;Failed

// ContainerActionState describes the outcome of an action that was requested
// via the AnnotationAction annotation.
type ContainerActionState string

// Possible values for ContainerActionState.
const (
	ContainerActionSucceeded ContainerActionState = "Succeeded"
	ContainerActionFailed    ContainerActionState = "Failed"
)

// ContainerLastAction records the most recent action requested via the
// AnnotationAction annotation and its outcome.
type ContainerLastAction struct {
	// Action is the action that was requested.
	//
	// +required
	Action ContainerAction `json:"action"`
	// State is the outcome of the action.
	//
	// +required
	State ContainerActionState `json:"state"`
	// Error is the error message if the action failed.
	//
	// +optional
	Error string `json:"error,omitempty"`
	// ObservedAt is when the reconciler began processing the action annotation.
	// Backlog may delay this relative to the user's write time, and dispatch
	// (especially restart's grace period) extends the gap to CompletedAt.
	//
	// +optional
	ObservedAt metav1.Time `json:"observedAt,omitempty,omitzero"`
	// CompletedAt is when the action completed, regardless of outcome.
	//
	// +optional
	CompletedAt metav1.Time `json:"completedAt,omitempty,omitzero"`
}

// ContainerPortBinding describes one host port for the container to bind to.
type ContainerPortBinding struct {
	// HostIP is the host IP address that the container's port is mapped to.
	//
	// +required
	HostIP string `json:"hostIP"`
	// HostPort is the host port number that the container's port is mapped to.
	//
	// +required
	HostPort string `json:"hostPort"`
}

// ContainerPort defines a single exposed port in a container.
type ContainerPort struct {
	// Name of the port; in the form [port]/[protocol], e.g. "80/tcp".
	//
	// +required
	Name string `json:"name"`
	// Bindings to the host port; empty for an exposed but unpublished port,
	// otherwise one entry per binding (e.g. IPv4 and IPv6).
	//
	// +optional
	Bindings []ContainerPortBinding `json:"bindings,omitempty"`
}

// ContainerSpec is currently empty. Actions are requested via the AnnotationAction
// annotation, not a level-triggered desired-state field. Docker's restart
// policy and out-of-band `docker start/stop` are competing writers, so a
// level-triggered desired state would fight them.
type ContainerSpec struct{}

// ContainerStatus defines the observed state of the container.
type ContainerStatus struct {
	// Name of the container; this is distinct from the container ID.
	//
	// +required
	Name string `json:"name"`
	// Namespace is the container namespace; refers to a `ContainerNamespace`
	// object in the same Kubernetes namespace.
	//
	// +required
	Namespace string `json:"namespace"`
	// Path to the executable (within the image) for the process.
	//
	// +required
	Path string `json:"path"`
	// Args is the arguments to the executable.
	//
	// +optional
	Args []string `json:"args"`
	// Image is the image the container was created with.
	//
	// +required
	Image string `json:"image"`
	// Ports describes the exposed ports of the container.
	//
	// +listType=map
	// +listMapKey=name
	// +optional
	Ports []ContainerPort `json:"ports,omitempty"`
	// Labels are the container labels.
	//
	// +optional
	Labels map[string]string `json:"labels"`
	// Status of the container.
	//
	// +required
	// +kubebuilder:default:=unknown
	Status ContainerStatusValue `json:"status"`
	// Pid is the process identifier for the main process in the container.
	//
	// +optional
	Pid int32 `json:"pid"`
	// ExitCode is the exit status of the main process in the container.
	//
	// +optional
	ExitCode int32 `json:"exitCode"`
	// Error message if the container has failed to start.
	//
	// +optional
	Error string `json:"error"`
	// CreatedAt is the time this container was initially created.
	//
	// +optional
	CreatedAt metav1.Time `json:"createdAt"`
	// StartedAt is the time this container was started; unset if the container is stopped.
	//
	// +optional
	StartedAt metav1.Time `json:"startedAt"`
	// FinishedAt is the time this container was last stopped; unset if the container never ran.
	//
	// +optional
	FinishedAt metav1.Time `json:"finishedAt"`
	// Conditions represent the calculated state of the container.
	// Each condition has a unique type and reflects the status of a specific aspect of the resource.
	//
	// Known condition types include:
	// - "Running": the container is running; this may also be paused.
	// - "Paused": the container is paused.
	// - "Restarting": the container is restarting.
	// - "OOMKilled": a process within this container has been killed because it ran out of memory
	//                since the container was last started.
	// - "Dead": the container is dead.
	//
	// The status of each condition is one of True, False, or Unknown.
	//
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
	// LastAction records the most recent action requested via the
	// AnnotationAction annotation and its outcome. Persists after the
	// action completes until overwritten by the next action.
	//
	// +optional
	LastAction *ContainerLastAction `json:"lastAction,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:ac:generate=true
// +kubebuilder:resource:categories="all"
// +kubebuilder:subresource:status
// +kubebuilder:selectablefield:JSONPath=.status.namespace
// +kubebuilder:printcolumn:name="Running",type=boolean,JSONPath=`.status.conditions[?(@.type=="Running")].status`

// Container is the Schema for the containers API.
type Container struct {
	metav1.TypeMeta `json:",inline"`

	// Metadata is a standard object metadata
	//
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty,omitzero"`

	// Spec is reserved for future use. The Container API has no
	// desired-state fields today: actions are requested via the
	// AnnotationAction annotation on metadata instead.
	//
	// +optional
	Spec ContainerSpec `json:"spec,omitempty,omitzero"`

	// Status defines the observed state of Container
	//
	// +optional
	Status ContainerStatus `json:"status,omitempty,omitzero"`
}

// +kubebuilder:object:root=true

// ContainerList contains a list of Container.
type ContainerList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Container `json:"items"`
}

// ContainerCreateRequestSpec defines the desired state for creating a container.
type ContainerCreateRequestSpec struct {
	// Name of the container to create; if not specified, a random name will be
	// generated.
	//
	// +optional
	Name string `json:"name"`
	// Namespace is the container namespace; refers to a `ContainerNamespace`
	// object in the same Kubernetes namespace.  If not specified, the container
	// will be created in the default namespace.
	//
	// +optional
	Namespace string `json:"namespace"`
	// Path is the path to the executable (within the image) for the process.
	// Defaults to the image's default command if not specified.
	//
	// +optional
	Path string `json:"path"`
	// Args is the arguments to the executable.
	//
	// +optional
	Args []string `json:"args"`
	// Image is the image the container was created with.
	// May be either a tag or a digest such as `sha256:dead00beef...`.
	//
	// +required
	Image string `json:"image"`
	// Ports describes the exposed ports of the container.
	//
	// +listType=map
	// +listMapKey=name
	// +optional
	Ports []ContainerPort `json:"ports,omitempty"`
	// Labels are the container labels.  They are merged with the image labels.
	//
	// +optional
	Labels map[string]string `json:"labels"`

	// State is the desired state of the container.
	//
	// +required
	// +kubebuilder:default:=running
	// +kubebuilder:validation:Enum=created;running
	State ContainerStatusValue `json:"state"`
}

// ContainerCreateRequestStatus defines the status for a container creation request.
type ContainerCreateRequestStatus struct {
	// Name is the name of the created container; this is the container ID.
	//
	// +required
	Name string `json:"name"`

	// Conditions represent the state of the container creation request.
	// Current known condition types include:
	// - "Complete": the container creation request has successfully completed.
	// - "Failed": the container creation request has failed.
	// The status of each condition is one of True, False, or Unknown.
	//
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:selectablefield:JSONPath=.spec.namespace
// +kubebuilder:metadata:annotations=rdd.rancherdesktop.io/controller=container

// ContainerCreateRequest defines a request to create a new container.
// After a container has been created, the ContainerCreateRequest object will
// be deleted after a short delay.
type ContainerCreateRequest struct {
	metav1.TypeMeta `json:",inline"`

	// Metadata is a standard object metadata
	//
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty,omitzero"`

	// Spec defines the desired state of Container
	//
	// +required
	Spec ContainerCreateRequestSpec `json:"spec"`

	// Status represents the current state of the ContainerCreateRequest
	//
	// +optional
	Status ContainerCreateRequestStatus `json:"status,omitempty,omitzero"`
}

// +kubebuilder:object:root=true

// ContainerCreateRequestList contains a list of ContainerCreateRequest.
type ContainerCreateRequestList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ContainerCreateRequest `json:"items"`
}

// ComposeProjectKind is the Kind string for ComposeProject resources.
const ComposeProjectKind = "ComposeProject"

// ComposeProject status conditions.
const (
	ComposeProjectConditionSettled    = "Settled"
	ComposeProjectConditionHasMembers = "HasMembers"
)

// ComposeProject status reasons for the Settled condition.
const (
	// ComposeProjectSettledReasonStarting indicates that the compose project is
	// in the process of starting up.
	ComposeProjectSettledReasonStarting = "Starting"
	// ComposeProjectSettledReasonStarted indicates that the compose project is
	// fully started and running.
	ComposeProjectSettledReasonStarted = "Started"
	// ComposeProjectSettledReasonErrored indicates that the compose project is
	// in an error state, and will not be able to start successfully.
	ComposeProjectSettledReasonErrored = "Errored"
	// ComposeProjectSettledReasonStopping indicates that the compose project is
	// in the process of shutting down.
	ComposeProjectSettledReasonStopping = "Stopping"
	// ComposeProjectSettledReasonStopped indicates that the compose project is
	// fully stopped, and will be deleted soon.
	ComposeProjectSettledReasonStopped = "Stopped"
)

// ComposeProject status reasons for the HasMembers condition.
const (
	// ComposeProjectHasMembersReasonFound indicates that the compose project has
	// at least one member.
	ComposeProjectHasMembersReasonFound = "Found"
	// ComposeProjectHasMembersReasonDeleted indicates that the last member of the
	// compose project has been removed; the project may be deleted soon.
	ComposeProjectHasMembersReasonDeleted = "Deleted"
	// ComposeProjectHasMembersReasonCalculating indicates that the controller is
	// still calculating whether the compose project has any members.
	ComposeProjectHasMembersReasonCalculating = "Calculating"
)

// +kubebuilder:validation:Enum=up;down

// ComposeProjectAction is the name of an action that can be requested on a
// ComposeProject via the AnnotationAction annotation.
type ComposeProjectAction string

// Possible values for ComposeProjectAction.
const (
	ComposeProjectActionUnset ComposeProjectAction = ""
	ComposeProjectActionUp    ComposeProjectAction = "up"
	ComposeProjectActionDown  ComposeProjectAction = "down"
)

// IsValid reports whether a is one of the defined ComposeProjectAction values.
func (a ComposeProjectAction) IsValid() bool {
	switch a {
	case ComposeProjectActionUnset, ComposeProjectActionUp, ComposeProjectActionDown:
		return true
	default:
		return false
	}
}

// +kubebuilder:validation:Enum=Succeeded;Failed

// ComposeProjectActionState describes the outcome of an action that was
// requested via the AnnotationAction annotation.
type ComposeProjectActionState string

// Possible values for ComposeProjectActionState.
const (
	ComposeProjectActionSucceeded ComposeProjectActionState = "Succeeded"
	ComposeProjectActionFailed    ComposeProjectActionState = "Failed"
)

// ComposeProjectLastAction records the most recent action requested via the
// AnnotationAction annotation and its outcome.
type ComposeProjectLastAction struct {
	// Action is the action that was requested.
	//
	// +required
	Action ComposeProjectAction `json:"action"`
	// State is the outcome of the action.
	//
	// +optional
	State ComposeProjectActionState `json:"state,omitempty"`
	// Error is the error message if the action failed.
	//
	// +optional
	Error string `json:"error,omitempty"`
	// ObservedAt is when the reconciler began processing the action
	// annotation.
	//
	// +optional
	ObservedAt metav1.Time `json:"observedAt,omitempty,omitzero"`
	// CompletedAt is when the action completed, regardless of outcome.
	//
	// +optional
	CompletedAt metav1.Time `json:"completedAt,omitempty,omitzero"`
}

// ComposeProjectSpec defines the identity of a `docker compose` project.
// The information here cannot recreate a running project (profile selection
// and environment variables set at `compose up` time are not available), so
// changing the spec does not automatically update the project; actions must
// be requested explicitly via the AnnotationAction annotation.
type ComposeProjectSpec struct {
	// Namespace is the container namespace; refers to a `ContainerNamespace`
	// object in the same Kubernetes namespace. Immutable once created.
	//
	// +required
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="spec.namespace is immutable"
	Namespace string `json:"namespace"`
	// Name is the compose project name. Immutable once created.
	//
	// +required
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="spec.name is immutable"
	Name string `json:"name"`
	// WorkingDir is the compose project directory on the host (i.e. relative
	// to where the RDD process runs). Used to look up any files needed. May
	// be unset for a ComposeProject that was automatically created from
	// observed containers or volumes.
	//
	// +optional
	WorkingDir string `json:"workingDir,omitempty"`
	// Configs is the list of compose files used to create the project,
	// relative to WorkingDir. It is not guaranteed that this is sufficient to
	// recreate the project. May be unset for a ComposeProject that was
	// automatically created from observed containers or volumes.
	//
	// +optional
	Configs []string `json:"configs,omitempty"`
}

// ComposeProjectMember describes a single member of a compose project, which is
// a container, image, or volume.
type ComposeProjectMember struct {
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

// ComposeProjectStatus defines the observed state of the compose project.
type ComposeProjectStatus struct {
	// LastAction records the most recent action requested via the
	// AnnotationAction annotation and its outcome. Persists after the action
	// completes until overwritten by the next action.
	//
	// +optional
	LastAction *ComposeProjectLastAction `json:"lastAction,omitempty"`
	// Members tracks the objects that are part of the compose project.  The name
	// is a combination of object kind and name, e.g. "Volume/myvolume".  The UID
	// is also tracked.
	//
	// +listType=map
	// +listMapKey=name
	// +optional
	Members []ComposeProjectMember `json:"members,omitempty"`
	// Conditions represent the state of the compose project.
	// Known condition types include:
	// - "Settled": The compose project is at a steady state.
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
// +kubebuilder:selectablefield:JSONPath=.spec.namespace
// +kubebuilder:selectablefield:JSONPath=.spec.name
// +kubebuilder:printcolumn:name="Project",type=string,JSONPath=`.spec.name`
// +kubebuilder:printcolumn:name="State",type=string,JSONPath=`.status.lastAction.state`

// ComposeProject is the Schema for the composeprojects API. ComposeProject
// objects do not reflect actual container engine objects; instead, they
// reflect `docker compose` projects.
type ComposeProject struct {
	metav1.TypeMeta `json:",inline"`

	// Metadata is a standard object metadata
	//
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty,omitzero"`

	// Spec defines the identity of the compose project.
	//
	// +required
	Spec ComposeProjectSpec `json:"spec"`

	// Status defines the observed state of ComposeProject
	//
	// +optional
	Status ComposeProjectStatus `json:"status,omitempty,omitzero"`
}

// +kubebuilder:object:root=true

// ComposeProjectList contains a list of ComposeProject.
type ComposeProjectList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ComposeProject `json:"items"`
}

func init() {
	registerTypes(
		&Container{}, &ContainerList{},
		&ContainerCreateRequest{}, &ContainerCreateRequestList{},
		&ComposeProject{}, &ComposeProjectList{},
	)
}
