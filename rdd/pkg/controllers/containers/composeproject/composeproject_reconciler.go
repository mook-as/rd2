// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: SUSE LLC
// SPDX-FileCopyrightText: The Rancher Desktop Authors

package composeproject

import (
	"context"
	"errors"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	"github.com/rancher-sandbox/rancher-desktop-daemon/pkg/apis/containers/v1alpha1"
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
	Kind string
	types.NamespacedName
	types.UID
}

// reconciler implements the ComposeProject reconcile loop.
type reconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=containers.rancherdesktop.io,resources=composeprojects,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=containers.rancherdesktop.io,resources=composeprojects/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=containers.rancherdesktop.io,resources=composeprojects/finalizers,verbs=update

// Reconcile dispatches requests by source kind.
func (r *reconciler) Reconcile(ctx context.Context, req composeProjectRequest) (ctrl.Result, error) {
	switch req.Kind {
	case v1alpha1.ComposeProjectKind:
		return ctrl.Result{}, nil
	case v1alpha1.ContainerKind:
		return ctrl.Result{}, nil
	case v1alpha1.ImageKind:
		return ctrl.Result{}, nil
	case v1alpha1.VolumeKind:
		return ctrl.Result{}, nil
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

// SetupWithManager sets up the controller with the Manager.
func (r *reconciler) SetupWithManager(mgr ctrl.Manager) error {
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
		Watches(&v1alpha1.Container{}, enqueueRequestsForKind(v1alpha1.ContainerKind), hasLabelPredicates).
		Watches(&v1alpha1.Image{}, enqueueRequestsForKind(v1alpha1.ImageKind), hasLabelPredicates).
		Watches(&v1alpha1.Volume{}, enqueueRequestsForKind(v1alpha1.VolumeKind), hasLabelPredicates).
		Watches(&v1alpha1.ComposeProject{}, enqueueRequestsForKind(v1alpha1.ComposeProjectKind)).
		Complete(r)
}
