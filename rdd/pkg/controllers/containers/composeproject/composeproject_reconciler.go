// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: SUSE LLC
// SPDX-FileCopyrightText: The Rancher Desktop Authors

package composeproject

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	k8sruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/source"

	"github.com/rancher-sandbox/rancher-desktop-daemon/pkg/apis/containers/v1alpha1"
	"github.com/rancher-sandbox/rancher-desktop-daemon/pkg/controllers/base"
)

// The standard `docker compose` labels set on containers (and other
// resources) that belong to a compose project.
const (
	composeProjectLabel     = "com.docker.compose.project"
	composeConfigHashLabel  = "com.docker.compose.config-hash"
	composeWorkingDirLabel  = "com.docker.compose.project.working_dir"
	composeConfigFilesLabel = "com.docker.compose.project.config_files"
)

// composeProjectRequest is the typed reconcile request for reconciler. It
// carries the originating kind in addition to the namespaced name and UID.
type composeProjectRequest struct {
	// The kind of the resource that triggered the reconcile request.
	Kind string
	// The namespaced name of the resource that triggered the reconcile request.
	types.NamespacedName
	// The UID of the resource that triggered the reconcile request.
	types.UID
}

// processCompletionEvent is the payload for a TypedGenericEvent indicating that
// a `docker compose` command has completed for a ComposeProject.
type processCompletionEvent struct {
	project *v1alpha1.ComposeProject
}

// processState tracks the state of a running `docker compose` command for a
// ComposeProject.
type processState struct {
	// The resource version of the process object when the process was started.
	resourceVersion string
	// The process being run.
	cmd command
	// Whether the process has terminated, successfully or not.
	finished bool
	// The error returned by the process, only valid if finished is true.
	err error
}

// reconciler implements the ComposeProject reconcile loop.
type reconciler struct {
	// The context that lasts for the lifetime of the reconciler.
	ctx context.Context
	client.Client
	Scheme *k8sruntime.Scheme
	// execCommand starts a process.
	execCommand commandExecutor
	// procs maps ComposeProject namespaced names to the currently-running
	// `docker compose` command for that project, if any.
	procs map[types.UID]processState
	// procsLock protects access to procs.
	procsLock sync.Mutex
	// procCompletionCh is a channel indicating that a `docker compose` command
	// has completed for a project, which triggers a reconcile.
	procCompletionCh chan event.TypedGenericEvent[processCompletionEvent]
}

// +kubebuilder:rbac:groups=containers.rancherdesktop.io,resources=composeprojects,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=containers.rancherdesktop.io,resources=composeprojects/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=containers.rancherdesktop.io,resources=composeprojects/finalizers,verbs=update
// +kubebuilder:rbac:groups=containers.rancherdesktop.io,resources=containers;images;volumes,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch

// Reconcile dispatches requests by source kind.
func (r *reconciler) Reconcile(ctx context.Context, req composeProjectRequest) (ctrl.Result, error) {
	switch req.Kind {
	case v1alpha1.ComposeProjectKind:
		return r.reconcileProject(ctx, req)
	case v1alpha1.ContainerKind, v1alpha1.ImageKind, v1alpha1.VolumeKind:
		return r.reconcileFromResource(ctx, req)
	default:
		logf.FromContext(ctx).Error(
			errors.New("unsupported reconcile request kind"),
			"Ignoring reconcile request",
			"kind", req.Kind,
			"name", req.Name,
			"namespace", req.Namespace,
		)
		return ctrl.Result{}, nil
	}
}

// reconcileProject handles a reconcile request for a ComposeProject resource.
func (r *reconciler) reconcileProject(ctx context.Context, req composeProjectRequest) (ctrl.Result, error) {
	var project v1alpha1.ComposeProject
	err := r.Get(ctx, req.NamespacedName, &project)
	if apierrors.IsNotFound(err) || (err == nil && project.GetUID() != req.UID) {
		// Either the project no longer exists, or a _newly recreated_ project is
		// now in its place; remove the process state if it exists.
		r.procsLock.Lock()
		state := r.procs[req.UID]
		delete(r.procs, req.UID)
		r.procsLock.Unlock()
		if !state.finished && state.cmd != nil {
			err = state.cmd.kill(ctx)
			logf.FromContext(ctx).V(1).Info("killed process for deleted compose project",
				"name", req.NamespacedName, "command", state.cmd.args(), "err", err)
		}
		return ctrl.Result{}, nil
	} else if err != nil {
		return ctrl.Result{}, err
	}

	// Handle action annotation, if present and valid.
	switch v1alpha1.ComposeProjectAction(project.Annotations[v1alpha1.AnnotationAction]) {
	case v1alpha1.ComposeProjectActionUp:
		if err := r.initiateProjectUp(ctx, &project); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	case v1alpha1.ComposeProjectActionDown:
		if err := r.initiateProjectDown(ctx, &project); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	// No action set; check if a process has completed.
	r.procsLock.Lock()
	state, hasState := r.procs[project.GetUID()]
	r.procsLock.Unlock()
	settled := apimeta.FindStatusCondition(project.Status.Conditions, v1alpha1.ComposeProjectConditionSettled)
	switch {
	case hasState && !state.finished:
		// The process is still running; wait for the reconcile when it exits.
	case hasState && settled == nil:
		// No condition set; we don't know why the process completed.  Remove the
		// state and wait for next reconcile.
		r.procsLock.Lock()
		delete(r.procs, project.GetUID())
		r.procsLock.Unlock()
	case settled != nil && settled.Reason == v1alpha1.ComposeProjectSettledReasonStarting:
		// `docker compose up` finished.
		if !hasState {
			// We lost track of the process; restart it to ensure it was done.  If we
			// had crashed, the process would have been aborted when we exited.
			if err := r.requestAction(ctx, &project, v1alpha1.ComposeProjectActionUp); err != nil {
				return ctrl.Result{}, err
			}
			break
		}
		if err := r.completeProjectUp(ctx, &project, state); err != nil {
			return ctrl.Result{}, fmt.Errorf(
				"failed to update status.conditions for compose project %s: %w", req.NamespacedName, err)
		}
		r.procsLock.Lock()
		delete(r.procs, project.GetUID())
		r.procsLock.Unlock()
	case settled != nil && settled.Reason == v1alpha1.ComposeProjectSettledReasonStopping:
		// `docker compose down` finished.
		if !hasState {
			// We lost track of the process; restart it to ensure it was done.  If we
			// had crashed, the process would have been aborted when we exited.
			if err := r.requestAction(ctx, &project, v1alpha1.ComposeProjectActionDown); err != nil {
				return ctrl.Result{}, err
			}
			break
		}
		if err := r.completeProjectDown(ctx, &project, state); err != nil {
			return ctrl.Result{}, fmt.Errorf(
				"failed to update status.conditions for compose project %s: %w", req.NamespacedName, err)
		}
		r.procsLock.Lock()
		delete(r.procs, project.GetUID())
		r.procsLock.Unlock()
	case hasState:
		// Process has completed for some other reason; remove the state and
		// wait for next reconcile.
		r.procsLock.Lock()
		delete(r.procs, project.GetUID())
		r.procsLock.Unlock()
	case apimeta.IsStatusConditionFalse(project.Status.Conditions, v1alpha1.ComposeProjectConditionHasMembers):
		// The project has no members, per the last reconcile.  Delete it.
		if err := r.Delete(ctx, &project); err != nil {
			return ctrl.Result{}, fmt.Errorf(
				"failed to delete compose project %s: %w", req.NamespacedName, err)
		}
	default:
		// The status was updated from a previous reconcile; update the `HasMembers`
		// condition to reflect the current state of the project.
		err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
			var latest v1alpha1.ComposeProject
			if err := r.Get(ctx, req.NamespacedName, &latest); err != nil {
				return err
			}
			var changed bool
			if len(latest.Status.Members) > 0 {
				changed = apimeta.SetStatusCondition(&latest.Status.Conditions, metav1.Condition{
					Type:    v1alpha1.ComposeProjectConditionHasMembers,
					Status:  metav1.ConditionTrue,
					Reason:  v1alpha1.ComposeProjectHasMembersReasonFound,
					Message: fmt.Sprintf("project has %d members", len(latest.Status.Members)),
				})
			} else {
				changed = apimeta.SetStatusCondition(&latest.Status.Conditions, metav1.Condition{
					Type:    v1alpha1.ComposeProjectConditionHasMembers,
					Status:  metav1.ConditionFalse,
					Reason:  v1alpha1.ComposeProjectHasMembersReasonDeleted,
					Message: "project has no members",
				})
			}
			if changed {
				return r.Status().Update(ctx, &latest)
			}
			return nil
		})
		if apierrors.IsNotFound(err) {
			// The project was deleted by another reconcile; nothing to do.
			return ctrl.Result{}, nil
		}
		if err != nil {
			return ctrl.Result{}, fmt.Errorf(
				"failed to update status.conditions for compose project %s: %w", req.NamespacedName, err)
		}
	}
	return ctrl.Result{}, nil
}

func (r *reconciler) requestAction(ctx context.Context, project *v1alpha1.ComposeProject, action v1alpha1.ComposeProjectAction) error {
	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		var latest v1alpha1.ComposeProject
		if err := r.Get(ctx, client.ObjectKeyFromObject(project), &latest); err != nil {
			return err
		}
		if latest.Annotations == nil {
			latest.Annotations = make(map[string]string)
		}
		latest.Annotations[v1alpha1.AnnotationAction] = string(action)
		return r.Update(ctx, &latest)
	})
	if err != nil {
		return fmt.Errorf(
			"failed to apply action annotation for compose project %s: %w", client.ObjectKeyFromObject(project), err)
	}
	return nil
}

// initiateProjectUp handles the `up` action.
func (r *reconciler) initiateProjectUp(ctx context.Context, project *v1alpha1.ComposeProject) error {
	// We encountered a "up" action; start `docker compose up`.  Abort the
	// previous process if it is still running; the user may have re-initiated the
	// action before the previous process completed.
	kill, err := r.runComposeCommand(r.ctx, project, "up", "--detach")
	if err != nil {
		return err
	}

	var currentResourceVersion string
	err = retry.RetryOnConflict(retry.DefaultRetry, func() error {
		var latest v1alpha1.ComposeProject
		if err := r.Get(ctx, client.ObjectKeyFromObject(project), &latest); err != nil {
			return err
		}
		apimeta.SetStatusCondition(&latest.Status.Conditions, metav1.Condition{
			Type:    v1alpha1.ComposeProjectConditionSettled,
			Status:  metav1.ConditionFalse,
			Reason:  v1alpha1.ComposeProjectSettledReasonStarting,
			Message: "project is being started",
		})
		apimeta.SetStatusCondition(&latest.Status.Conditions, metav1.Condition{
			Type:    v1alpha1.ComposeProjectConditionHasMembers,
			Status:  metav1.ConditionUnknown,
			Reason:  v1alpha1.ComposeProjectHasMembersReasonCalculating,
			Message: "project is being started",
		})
		latest.Status.LastAction = &v1alpha1.ComposeProjectLastAction{
			Action:     v1alpha1.ComposeProjectActionUp,
			ObservedAt: metav1.Now(),
		}
		if err := r.Status().Update(ctx, &latest); err != nil {
			return err
		}
		currentResourceVersion = latest.ResourceVersion
		return nil
	})
	if err != nil {
		// Failed to apply; abort the just-started process, we will try again on reconcile.
		_ = kill(ctx)
		return fmt.Errorf(
			"failed to apply status for compose project %s: %w", client.ObjectKeyFromObject(project), err)
	}

	err = retry.RetryOnConflict(retry.DefaultRetry, func() error {
		var latest v1alpha1.ComposeProject
		if err := r.Get(ctx, client.ObjectKeyFromObject(project), &latest); err != nil {
			return err
		}
		if latest.Annotations == nil {
			latest.Annotations = make(map[string]string)
		}
		latest.Annotations[v1alpha1.AnnotationAction] = string(v1alpha1.ComposeProjectActionUnset)
		if err := r.Update(ctx, &latest); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		// Failed to update annotations; update the tracked resourceVersion, so when
		// we try doing this again next reconcile, we don't restart the process.
		// This is not needed if we succeed to update the annotations, because in
		// that case if we get here again the user has manually restarted the action.
		if currentResourceVersion == "" {
			// We didn't get the resource version from the apply; just abort the
			// process, so we can try the whole thing again next reconcile.
			_ = kill(ctx)
		} else {
			r.procsLock.Lock()
			state := r.procs[project.GetUID()]
			state.resourceVersion = currentResourceVersion
			r.procs[project.GetUID()] = state
			r.procsLock.Unlock()
		}
		return fmt.Errorf(
			"failed to apply annotations for compose project %s: %w", client.ObjectKeyFromObject(project), err)
	}
	return nil
}

// completeProjectUp handles the completion of a `docker compose up` command for a ComposeProject.
func (r *reconciler) completeProjectUp(ctx context.Context, project *v1alpha1.ComposeProject, state processState) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		var latest v1alpha1.ComposeProject
		if err := r.Get(ctx, client.ObjectKeyFromObject(project), &latest); err != nil {
			return err
		}
		if state.err != nil {
			apimeta.SetStatusCondition(&latest.Status.Conditions, metav1.Condition{
				Type:    v1alpha1.ComposeProjectConditionSettled,
				Status:  metav1.ConditionTrue,
				Reason:  v1alpha1.ComposeProjectSettledReasonErrored,
				Message: fmt.Sprintf("project failed to start: %v", state.err),
			})
			// Update LastAction, but leave ObservedAt alone.
			var observedAt metav1.Time
			if latest.Status.LastAction != nil {
				observedAt = latest.Status.LastAction.ObservedAt
			}
			latest.Status.LastAction = &v1alpha1.ComposeProjectLastAction{
				Action:      v1alpha1.ComposeProjectActionUp,
				State:       v1alpha1.ComposeProjectActionFailed,
				Error:       state.cmd.output(),
				ObservedAt:  observedAt,
				CompletedAt: metav1.Now(),
			}
		} else {
			apimeta.SetStatusCondition(&latest.Status.Conditions, metav1.Condition{
				Type:    v1alpha1.ComposeProjectConditionSettled,
				Status:  metav1.ConditionTrue,
				Reason:  v1alpha1.ComposeProjectSettledReasonStarted,
				Message: "project has been started",
			})
			// Update LastAction, but leave ObservedAt alone.
			var observedAt metav1.Time
			if latest.Status.LastAction != nil {
				observedAt = latest.Status.LastAction.ObservedAt
			}
			latest.Status.LastAction = &v1alpha1.ComposeProjectLastAction{
				Action:      v1alpha1.ComposeProjectActionUp,
				State:       v1alpha1.ComposeProjectActionSucceeded,
				ObservedAt:  observedAt,
				CompletedAt: metav1.Now(),
			}
		}
		return r.Status().Update(ctx, &latest)
	})
}

// initiateProjectDown handles the `down` action.
func (r *reconciler) initiateProjectDown(ctx context.Context, project *v1alpha1.ComposeProject) error {
	// We encountered a "down" action; start `docker compose down`.  Abort the
	// previous process if it is still running; the user may have re-initiated the
	// action before the previous process completed.
	kill, err := r.runComposeCommand(r.ctx, project, "down", "--remove-orphans", "--volumes")
	if err != nil {
		return err
	}

	var currentResourceVersion string
	err = retry.RetryOnConflict(retry.DefaultRetry, func() error {
		var latest v1alpha1.ComposeProject
		if err := r.Get(ctx, client.ObjectKeyFromObject(project), &latest); err != nil {
			return err
		}
		apimeta.SetStatusCondition(&latest.Status.Conditions, metav1.Condition{
			Type:    v1alpha1.ComposeProjectConditionSettled,
			Status:  metav1.ConditionFalse,
			Reason:  v1alpha1.ComposeProjectSettledReasonStopping,
			Message: "project is being torn down",
		})
		apimeta.SetStatusCondition(&latest.Status.Conditions, metav1.Condition{
			Type:    v1alpha1.ComposeProjectConditionHasMembers,
			Status:  metav1.ConditionUnknown,
			Reason:  v1alpha1.ComposeProjectHasMembersReasonCalculating,
			Message: "project is being torn down",
		})
		latest.Status.LastAction = &v1alpha1.ComposeProjectLastAction{
			Action:     v1alpha1.ComposeProjectActionDown,
			ObservedAt: metav1.Now(),
		}
		if err := r.Status().Update(ctx, &latest); err != nil {
			return err
		}
		currentResourceVersion = latest.ResourceVersion
		return nil
	})
	if err != nil {
		// Failed to apply; abort the just-started process, we will try again on reconcile.
		_ = kill(ctx)
		return fmt.Errorf(
			"failed to apply status for compose project %s: %w", client.ObjectKeyFromObject(project), err)
	}

	err = retry.RetryOnConflict(retry.DefaultRetry, func() error {
		var latest v1alpha1.ComposeProject
		if err := r.Get(ctx, client.ObjectKeyFromObject(project), &latest); err != nil {
			return err
		}
		if latest.Annotations == nil {
			latest.Annotations = make(map[string]string)
		}
		latest.Annotations[v1alpha1.AnnotationAction] = string(v1alpha1.ComposeProjectActionUnset)
		if err := r.Update(ctx, &latest); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		// Failed to update annotations; update the tracked resourceVersion, so when
		// we try doing this again next reconcile, we don't restart the process.
		// This is not needed if we succeed to update the annotations, because in
		// that case if we get here again the user has manually restarted the action.
		if currentResourceVersion == "" {
			// We didn't get the resource version from the apply; just abort the
			// process, so we can try the whole thing again next reconcile.
			_ = kill(ctx)
		} else {
			r.procsLock.Lock()
			state := r.procs[project.GetUID()]
			state.resourceVersion = currentResourceVersion
			r.procs[project.GetUID()] = state
			r.procsLock.Unlock()
		}
		return fmt.Errorf(
			"failed to apply annotations for compose project %s: %w", client.ObjectKeyFromObject(project), err)
	}
	return nil
}

func (r *reconciler) completeProjectDown(ctx context.Context, project *v1alpha1.ComposeProject, state processState) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		var latest v1alpha1.ComposeProject
		if err := r.Get(ctx, client.ObjectKeyFromObject(project), &latest); err != nil {
			return err
		}
		if state.err != nil {
			apimeta.SetStatusCondition(&latest.Status.Conditions, metav1.Condition{
				Type:    v1alpha1.ComposeProjectConditionSettled,
				Status:  metav1.ConditionTrue,
				Reason:  v1alpha1.ComposeProjectSettledReasonErrored,
				Message: fmt.Sprintf("project failed to stop: %v", state.err),
			})
			// Update LastAction, but leave ObservedAt alone.
			var observedAt metav1.Time
			if latest.Status.LastAction != nil {
				observedAt = latest.Status.LastAction.ObservedAt
			}
			latest.Status.LastAction = &v1alpha1.ComposeProjectLastAction{
				Action:      v1alpha1.ComposeProjectActionDown,
				State:       v1alpha1.ComposeProjectActionFailed,
				Error:       state.cmd.output(),
				ObservedAt:  observedAt,
				CompletedAt: metav1.Now(),
			}
		} else {
			apimeta.SetStatusCondition(&latest.Status.Conditions, metav1.Condition{
				Type:    v1alpha1.ComposeProjectConditionSettled,
				Status:  metav1.ConditionTrue,
				Reason:  v1alpha1.ComposeProjectSettledReasonStopped,
				Message: "project has been torn down",
			})
			// Update LastAction, but leave ObservedAt alone.
			var observedAt metav1.Time
			if latest.Status.LastAction != nil {
				observedAt = latest.Status.LastAction.ObservedAt
			}
			latest.Status.LastAction = &v1alpha1.ComposeProjectLastAction{
				Action:      v1alpha1.ComposeProjectActionDown,
				State:       v1alpha1.ComposeProjectActionSucceeded,
				ObservedAt:  observedAt,
				CompletedAt: metav1.Now(),
			}
		}
		return r.Status().Update(ctx, &latest)
	})
}

// runComposeCommand runs a `docker compose` command for the given
// ComposeProject, aborting any previously-running command for that project.
// This is a no-op if the previously-running command was started for the same
// resource version of the project, indicating that this is a duplicate
// reconcile.  The args are passed to `docker compose` after the "compose"
// subcommand and the project's own --project-name/--file flags.
// Returns a function that aborts the command.
func (r *reconciler) runComposeCommand(ctx context.Context, project *v1alpha1.ComposeProject, args ...string) (func(context.Context) error, error) {
	// TODO: Support nerdctl here
	cli := "docker"

	// Compose derives the project name and looks up config files relative to
	// the current directory by default; pass the project's own identity
	// explicitly so this always targets the right project, regardless of
	// WorkingDir's directory name or which config file names are in use.
	composeArgs := []string{"compose", "--project-name", project.Spec.Name}
	for _, config := range project.Spec.Configs {
		composeArgs = append(composeArgs, "--file", config)
	}
	composeArgs = append(composeArgs, args...)

	r.procsLock.Lock()
	if r.procs == nil {
		r.procs = make(map[types.UID]processState)
	}
	state := r.procs[project.GetUID()]
	r.procsLock.Unlock()
	if state.cmd != nil {
		// A command already exists.  Check if it's from the same resource version;
		// if so, then this is a retry.
		if state.resourceVersion == project.GetResourceVersion() {
			// The resource version hasn't changed since the process was started;
			// this is a duplicate reconcile request.  Don't restart the process.
			// The caller may still abort the process if updating the status fails;
			// that is fine as it is an error if the status is out of sync.
			return state.cmd.kill, nil
		}
		// Not the same resource version; abort if the command is still running.
		if !state.finished {
			state.err = state.cmd.kill(ctx)
			if state.err != nil {
				return nil, fmt.Errorf("failed to kill existing compose command for project %s: %w", project.Name, state.err)
			}
		}
	}
	// Clear any previous state; they are no longer relevant.
	state = processState{
		resourceVersion: project.GetResourceVersion(),
	}
	var err error
	state.cmd, err = r.execCommand(ctx, project.Spec.WorkingDir, cli, composeArgs...)
	if err != nil {
		return nil, fmt.Errorf("failed to run compose command for project %s: %w", project.Name, err)
	}
	r.procsLock.Lock()
	r.procs[project.GetUID()] = state
	r.procsLock.Unlock()
	go func() {
		// `ctx` only lasts for this reconcile; use the controller lifetime instead.
		err := state.cmd.wait(r.ctx)
		state.err = err
		state.finished = true
		// Update the state, but only if the state hasn't changed since we started the command.
		r.procsLock.Lock()
		if currentState, ok := r.procs[project.GetUID()]; ok && currentState.cmd == state.cmd {
			r.procs[project.GetUID()] = state
		}
		r.procsLock.Unlock()
		completionEvent := event.TypedGenericEvent[processCompletionEvent]{
			Object: processCompletionEvent{
				project: project,
			},
		}
		select {
		case r.procCompletionCh <- completionEvent:
		case <-time.After(time.Second):
			// The channel has a pretty big buffer; if it's still full, just ignore it
			// and hope something else will trigger a reconcile.
			logf.FromContext(ctx).V(1).Info(
				"failed to send process completion event for compose project",
				"name", project.Name, "namespace", project.Namespace)
		}
	}()
	return state.cmd.kill, nil
}

// reconcileFromResource creates or updates a ComposeProject, as identified by
// the request.
func (r *reconciler) reconcileFromResource(ctx context.Context, req composeProjectRequest) (ctrl.Result, error) {
	// Find the object (Container, Image, or Volume) that triggered the reconcile request.
	obj := unstructured.Unstructured{}
	obj.SetGroupVersionKind(v1alpha1.SchemeGroupVersion.WithKind(req.Kind))
	if err := r.Get(ctx, req.NamespacedName, &obj); err != nil {
		if apierrors.IsNotFound(err) {
			// The resource was deleted, so remove its membership from any
			// ComposeProject in the same namespace.
			return r.removeComposeProjectMembership(ctx, req)
		}
		return ctrl.Result{}, err
	}
	namespace, _, err := unstructured.NestedString(obj.Object, "status", "namespace")
	if err != nil || namespace == "" {
		return ctrl.Result{}, fmt.Errorf("failed to get status.namespace for %s %s: %w", req.Kind, req.NamespacedName, err)
	}
	labels, _, err := unstructured.NestedStringMap(obj.Object, "status", "labels")
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to get status.labels for %s %s: %w", req.Kind, req.NamespacedName, err)
	}
	projectName := labels[composeProjectLabel]
	if projectName == "" {
		return r.removeComposeProjectMembership(ctx, req)
	}
	if labels[composeConfigHashLabel] == "" {
		// The resource is not part of a compose project, so remove its membership
		// from any ComposeProject in the same namespace.
		return r.removeComposeProjectMembership(ctx, req)
	}

	project := &v1alpha1.ComposeProject{
		ObjectMeta: metav1.ObjectMeta{
			Name:      generateProjectName(namespace, projectName),
			Namespace: obj.GetNamespace(),
		},
	}

	_, err = controllerutil.CreateOrUpdate(ctx, r.Client, project, func() error {
		project.Spec.Namespace = namespace
		project.Spec.Name = projectName
		if labels[composeWorkingDirLabel] != "" {
			project.Spec.WorkingDir = labels[composeWorkingDirLabel]
		}
		configs := composeConfigFiles(labels[composeWorkingDirLabel], labels[composeConfigFilesLabel])
		if len(configs) > 0 {
			project.Spec.Configs = configs
		}
		return nil
	})
	logf.FromContext(ctx).V(1).Info(
		"created or updated compose project from resource",
		"kind", req.Kind,
		"resource", req.NamespacedName,
		"project", types.NamespacedName{Namespace: namespace, Name: projectName},
		"err", err)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf(
			"failed to create or update compose project for %s %s: %w", req.Kind, obj.GetName(), err)
	}

	currentMember := fmt.Sprintf("%s/%s", req.Kind, obj.GetName())
	err = retry.RetryOnConflict(retry.DefaultRetry, func() error {
		var latest v1alpha1.ComposeProject
		if err := r.Get(ctx, client.ObjectKeyFromObject(project), &latest); err != nil {
			return err
		}
		index := slices.IndexFunc(latest.Status.Members, func(m v1alpha1.ComposeProjectMember) bool {
			return m.Name == currentMember
		})
		if index >= 0 {
			latest.Status.Members[index].UID = obj.GetUID()
		} else {
			latest.Status.Members = append(latest.Status.Members, v1alpha1.ComposeProjectMember{
				Name: currentMember,
				UID:  obj.GetUID(),
			})
		}
		apimeta.SetStatusCondition(&latest.Status.Conditions, metav1.Condition{
			Type:    v1alpha1.ComposeProjectConditionHasMembers,
			Status:  metav1.ConditionTrue,
			Reason:  v1alpha1.ComposeProjectHasMembersReasonFound,
			Message: fmt.Sprintf("last found member: %s %s", req.Kind, obj.GetName()),
		})
		return r.Status().Update(ctx, &latest)
	})
	logf.FromContext(ctx).V(1).Info(
		"updated compose project status from resource",
		"kind", req.Kind,
		"resource", req.NamespacedName,
		"project", types.NamespacedName{Namespace: namespace, Name: projectName},
		"err", err)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf(
			"failed to update status for compose project %s: %w", project.Name, err)
	}

	return ctrl.Result{}, nil
}

// removeComposeProjectMembership removes uid from any ComposeProject in the
// given Kubernetes namespace that has it recorded as a member for kind; that
// project's `HasMembers` status condition is updated accordingly.  If uid is
// empty, this is a no-op.
func (r *reconciler) removeComposeProjectMembership(ctx context.Context, req composeProjectRequest) (ctrl.Result, error) {
	if req.UID == "" {
		return ctrl.Result{}, nil
	}

	indexKey := ".status.members[*].uid"
	var list v1alpha1.ComposeProjectList
	if err := r.List(ctx, &list, client.InNamespace(req.Namespace), client.MatchingFields{indexKey: string(req.UID)}); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to list compose projects with member UID %s: %w", req.UID, err)
	}

	for _, item := range list.Items {
		itemKey := client.ObjectKeyFromObject(&item)
		err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
			var latest v1alpha1.ComposeProject
			if err := r.Get(ctx, itemKey, &latest); err != nil {
				return client.IgnoreNotFound(err)
			}
			newMembers := slices.DeleteFunc(latest.Status.Members, func(m v1alpha1.ComposeProjectMember) bool {
				return m.UID == req.UID
			})
			latest.Status.Members = newMembers
			apimeta.SetStatusCondition(&latest.Status.Conditions, metav1.Condition{
				Type:    v1alpha1.ComposeProjectConditionHasMembers,
				Status:  metav1.ConditionUnknown,
				Reason:  v1alpha1.ComposeProjectHasMembersReasonCalculating,
				Message: fmt.Sprintf("removing member: %s %s", req.Kind, req.Name),
			})
			return r.Status().Update(ctx, &latest)
		})
		if err != nil {
			return ctrl.Result{}, fmt.Errorf(
				"failed to update status for compose project %s: %w", item.Name, err)
		}
	}

	return ctrl.Result{}, nil
}

// composeConfigFiles parses the comma-separated list of absolute compose
// file paths from the composeConfigFilesLabel value, and returns them
// relative to workingDir, matching ComposeProjectSpec.Configs's documented
// convention. Paths that cannot be made relative to workingDir (or when
// workingDir is unknown) are returned unchanged.
func composeConfigFiles(workingDir, rawConfigFiles string) []string {
	if rawConfigFiles == "" {
		return nil
	}

	paths := strings.Split(rawConfigFiles, ",")
	configs := make([]string, 0, len(paths))
	for _, path := range paths {
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}
		if workingDir == "" {
			configs = append(configs, path)
			continue
		}
		rel, err := filepath.Rel(workingDir, path)
		if err != nil || strings.HasPrefix(rel, "..") {
			configs = append(configs, path)
			continue
		}
		configs = append(configs, rel)
	}

	return configs
}

// SetupWithManager sets up the controller with the Manager.
func (r *reconciler) SetupWithManager(ctx context.Context, mgr ctrl.Manager) error {
	if err := base.IndexFields(ctx, &v1alpha1.ComposeProject{}, mgr); err != nil {
		return err
	}
	// status.members is an array of ComposeProjectMember, which cannot be indexed
	// via the CRD directly.  Instead, set up a client-side index so we can find
	// the project given a UID of a member resource.  This index will be used to
	// find the correct project to remove a member from when that member is
	// deleted.
	if err := base.IndexField(ctx, &v1alpha1.ComposeProject{}, mgr, ".status.members[*].uid"); err != nil {
		return err
	}

	enqueueRequestsForKind := func(kind string) handler.TypedEventHandler[client.Object, composeProjectRequest] {
		return handler.TypedEnqueueRequestsFromMapFunc(func(_ context.Context, obj client.Object) []composeProjectRequest {
			return []composeProjectRequest{{
				Kind:           kind,
				NamespacedName: client.ObjectKeyFromObject(obj),
				UID:            obj.GetUID(),
			}}
		})
	}

	hasLabelFunc := func(object client.Object) bool {
		var objectLabels map[string]string
		switch o := object.(type) {
		case *v1alpha1.Container:
			objectLabels = o.Status.Labels
		case *v1alpha1.Image:
			objectLabels = o.Status.Labels
		case *v1alpha1.Volume:
			objectLabels = o.Status.Labels
		}
		_, ok := objectLabels[composeProjectLabel]
		return ok
	}

	// hasLabelPredicates filters events to only those that have the
	// composeProjectLabel set in the status.labels of the object.  We do not use
	// [predicate.NewPredicateFuncs] because it doesn't check the old object on
	// update.
	hasLabelPredicates := builder.WithPredicates(predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool {
			return hasLabelFunc(e.Object)
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			return hasLabelFunc(e.ObjectNew) || hasLabelFunc(e.ObjectOld)
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			return hasLabelFunc(e.Object)
		},
		GenericFunc: func(e event.GenericEvent) bool {
			return hasLabelFunc(e.Object)
		},
	})

	return builder.TypedControllerManagedBy[composeProjectRequest](mgr).
		Named("composeproject-reconciler").
		WatchesRawSource(source.TypedChannel(r.procCompletionCh,
			handler.TypedEnqueueRequestsFromMapFunc(func(_ context.Context, e processCompletionEvent) []composeProjectRequest {
				return []composeProjectRequest{{
					Kind:           v1alpha1.ComposeProjectKind,
					NamespacedName: client.ObjectKeyFromObject(e.project),
					UID:            e.project.GetUID(),
				}}
			}))).
		Watches(&v1alpha1.Container{}, enqueueRequestsForKind(v1alpha1.ContainerKind), hasLabelPredicates).
		Watches(&v1alpha1.Image{}, enqueueRequestsForKind(v1alpha1.ImageKind), hasLabelPredicates).
		Watches(&v1alpha1.Volume{}, enqueueRequestsForKind(v1alpha1.VolumeKind), hasLabelPredicates).
		Watches(&v1alpha1.ComposeProject{}, enqueueRequestsForKind(v1alpha1.ComposeProjectKind)).
		Complete(r)
}
