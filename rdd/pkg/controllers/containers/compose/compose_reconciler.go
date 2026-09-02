// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: SUSE LLC
// SPDX-FileCopyrightText: The Rancher Desktop Authors

package compose

import (
	"context"
	"errors"

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

// composeRequest is the typed reconcile request for reconciler. It carries
// the originating kind in addition to the namespaced name and UID.
type composeRequest struct {
	Kind string
	types.NamespacedName
	types.UID
}

// reconciler implements the ComposeProject reconcile loop.
type reconciler struct {
	client.Client
}

// +kubebuilder:rbac:groups=containers.rancherdesktop.io,resources=composeprojects,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=containers.rancherdesktop.io,resources=composeprojects/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=containers.rancherdesktop.io,resources=composeprojects/finalizers,verbs=update

// Reconcile dispatches requests by source kind.
func (r *reconciler) Reconcile(ctx context.Context, req composeRequest) (ctrl.Result, error) {
	switch req.Kind {
	case v1alpha1.ComposeProjectKind:
		return ctrl.Result{}, nil
	case v1alpha1.ContainerKind:
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
	hasLabelFunc := func(obj client.Object) bool {
		container, ok := obj.(*v1alpha1.Container)
		return ok && container.Status.Labels[composeProjectLabel] != ""
	}

	return builder.TypedControllerManagedBy[composeRequest](mgr).
		Named("compose-reconciler").
		Watches(&v1alpha1.Container{}, handler.TypedEnqueueRequestsFromMapFunc(func(_ context.Context, obj client.Object) []composeRequest {
			return []composeRequest{{
				Kind:           v1alpha1.ContainerKind,
				NamespacedName: client.ObjectKeyFromObject(obj),
				UID:            obj.GetUID(),
			}}
		}), builder.WithPredicates(predicate.Funcs{
			CreateFunc: func(obj event.TypedCreateEvent[client.Object]) bool {
				return hasLabelFunc(obj.Object)
			},
			UpdateFunc: func(obj event.TypedUpdateEvent[client.Object]) bool {
				return hasLabelFunc(obj.ObjectNew) || hasLabelFunc(obj.ObjectOld)
			},
			DeleteFunc: func(obj event.TypedDeleteEvent[client.Object]) bool {
				return hasLabelFunc(obj.Object)
			},
			GenericFunc: func(obj event.TypedGenericEvent[client.Object]) bool {
				return hasLabelFunc(obj.Object)
			},
		})).
		Watches(&v1alpha1.ComposeProject{}, handler.TypedEnqueueRequestsFromMapFunc(func(_ context.Context, obj client.Object) []composeRequest {
			return []composeRequest{{
				Kind:           v1alpha1.ComposeProjectKind,
				NamespacedName: client.ObjectKeyFromObject(obj),
				UID:            obj.GetUID(),
			}}
		})).
		Complete(r)
}
