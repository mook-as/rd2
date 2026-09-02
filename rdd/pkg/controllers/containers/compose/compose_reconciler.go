// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: SUSE LLC
// SPDX-FileCopyrightText: The Rancher Desktop Authors

package compose

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

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

// composeRequest is the typed reconcile request for reconciler. It carries
// the originating kind in addition to the namespaced name and UID.
type composeRequest struct {
	Kind string
	types.NamespacedName
	types.UID
}

// reconciler implements the Compose reconcile loop.
type reconciler struct {
	client.Client
}

// +kubebuilder:rbac:groups=containers.rancherdesktop.io,resources=composes,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=containers.rancherdesktop.io,resources=composes/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=containers.rancherdesktop.io,resources=composes/finalizers,verbs=update
// +kubebuilder:rbac:groups=containers.rancherdesktop.io,resources=containers;images;volumes,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch

// Reconcile dispatches requests by source kind.
func (r *reconciler) Reconcile(ctx context.Context, req composeRequest) (ctrl.Result, error) {
	switch req.Kind {
	case v1alpha1.ComposeKind:
		return ctrl.Result{}, nil
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

// reconcileFromResource creates or updates a Compose object, as identified by
// the request.
func (r *reconciler) reconcileFromResource(ctx context.Context, req composeRequest) (ctrl.Result, error) {
	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		// Find the object (Container, Image, or Volume) that triggered the reconcile request.
		obj := unstructured.Unstructured{}
		obj.SetGroupVersionKind(v1alpha1.SchemeGroupVersion.WithKind(req.Kind))
		if err := r.Get(ctx, req.NamespacedName, &obj); err != nil {
			if apierrors.IsNotFound(err) {
				// The resource was deleted, so remove its membership from any
				// Compose in the same namespace.
				return r.removeComposeMembership(ctx, req)
			}
			return err
		}
		namespace, _, err := unstructured.NestedString(obj.Object, "status", "namespace")
		if err != nil || namespace == "" {
			return fmt.Errorf("failed to get status.namespace for %s %s: %w", req.Kind, req.NamespacedName, err)
		}
		labels, _, err := unstructured.NestedStringMap(obj.Object, "status", "labels")
		if err != nil {
			return fmt.Errorf("failed to get status.labels for %s %s: %w", req.Kind, req.NamespacedName, err)
		}
		projectName := labels[composeProjectLabel]
		if projectName == "" {
			return r.removeComposeMembership(ctx, req)
		}
		if labels[composeConfigHashLabel] == "" {
			// The resource is not part of a compose project, so remove its membership
			// from any Compose in the same namespace.
			return r.removeComposeMembership(ctx, req)
		}

		project := &v1alpha1.Compose{
			ObjectMeta: metav1.ObjectMeta{
				Name:      generateProjectName(namespace, projectName),
				Namespace: obj.GetNamespace(),
			},
		}

		// Create the Compose project if it doesn't exist yet.
		//nolint:gocritic // Linter doesn't understand IgnoreAlreadyExists
		if err := r.Create(ctx, project); client.IgnoreAlreadyExists(err) != nil {
			return fmt.Errorf(
				"failed to create compose project for %s %s: %w", req.Kind, obj.GetName(), err)
		}

		currentMember := fmt.Sprintf("%s/%s", req.Kind, obj.GetName())
		if err := r.Get(ctx, client.ObjectKeyFromObject(project), project); err != nil {
			return err
		}
		project.Status.Namespace = namespace
		project.Status.Name = projectName
		if labels[composeWorkingDirLabel] != "" {
			project.Status.WorkingDir = labels[composeWorkingDirLabel]
		}
		configs := composeConfigFiles(labels[composeWorkingDirLabel], labels[composeConfigFilesLabel])
		if len(configs) > 0 {
			project.Status.Configs = configs
		}
		index := slices.IndexFunc(project.Status.Members, func(m v1alpha1.ComposeMember) bool {
			return m.Name == currentMember
		})
		if index >= 0 {
			project.Status.Members[index].UID = obj.GetUID()
		} else {
			project.Status.Members = append(project.Status.Members, v1alpha1.ComposeMember{
				Name: currentMember,
				UID:  obj.GetUID(),
			})
		}
		apimeta.SetStatusCondition(&project.Status.Conditions, metav1.Condition{
			Type:    v1alpha1.ComposeConditionHasMembers,
			Status:  metav1.ConditionTrue,
			Reason:  v1alpha1.ComposeHasMembersReasonFound,
			Message: fmt.Sprintf("last found member: %s %s", req.Kind, obj.GetName()),
		})
		return r.Status().Update(ctx, project)
	})
	return ctrl.Result{}, err
}

// removeComposeMembership removes the object referred to in the given request
// from the `HasMembers` status of any Compose project in the same namespace.
func (r *reconciler) removeComposeMembership(ctx context.Context, req composeRequest) error {
	indexKey := ".status.members[*].name"
	matchKey := fmt.Sprintf("%s/%s", req.Kind, req.Name)
	var list v1alpha1.ComposeList
	if err := r.List(ctx, &list, client.InNamespace(req.Namespace), client.MatchingFields{indexKey: matchKey}); err != nil {
		return fmt.Errorf("failed to list compose projects with member %s: %w", matchKey, err)
	}

	for _, item := range list.Items {
		itemKey := client.ObjectKeyFromObject(&item)
		var latest v1alpha1.Compose
		if err := r.Get(ctx, itemKey, &latest); err != nil {
			return client.IgnoreNotFound(err)
		}
		newMembers := slices.DeleteFunc(latest.Status.Members, func(m v1alpha1.ComposeMember) bool {
			if req.UID != "" && m.UID != req.UID {
				return false
			}
			return m.Name == matchKey
		})
		latest.Status.Members = newMembers
		apimeta.SetStatusCondition(&latest.Status.Conditions, metav1.Condition{
			Type:    v1alpha1.ComposeConditionHasMembers,
			Status:  metav1.ConditionUnknown,
			Reason:  v1alpha1.ComposeHasMembersReasonCalculating,
			Message: fmt.Sprintf("removing member: %s %s", req.Kind, req.Name),
		})
		if err := r.Status().Update(ctx, &latest); err != nil {
			return err
		}
	}

	return nil
}

// composeConfigFiles parses the comma-separated list of absolute compose
// file paths from the composeConfigFilesLabel value, and returns them
// relative to workingDir, matching ComposeStatus.Configs's documented
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
	if err := base.IndexFields(ctx, &v1alpha1.Compose{}, mgr); err != nil {
		return err
	}
	// status.members is an array of ComposeMember, which cannot be indexed via
	// the CRD directly.  Instead, set up a client-side index so we can find the
	// project given a member resource.  This index will be used to find the
	// correct project to remove a member from when that member is deleted.
	if err := base.IndexField(ctx, &v1alpha1.Compose{}, mgr, ".status.members[*].name"); err != nil {
		return err
	}

	enqueueRequestsForKind := func(kind string) handler.TypedEventHandler[client.Object, composeRequest] {
		return handler.TypedEnqueueRequestsFromMapFunc(func(_ context.Context, obj client.Object) []composeRequest {
			return []composeRequest{{
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

	return builder.TypedControllerManagedBy[composeRequest](mgr).
		Named("compose-reconciler").
		Watches(&v1alpha1.Container{}, enqueueRequestsForKind(v1alpha1.ContainerKind), hasLabelPredicates).
		Watches(&v1alpha1.Image{}, enqueueRequestsForKind(v1alpha1.ImageKind), hasLabelPredicates).
		Watches(&v1alpha1.Volume{}, enqueueRequestsForKind(v1alpha1.VolumeKind), hasLabelPredicates).
		Watches(&v1alpha1.Compose{}, enqueueRequestsForKind(v1alpha1.ComposeKind)).
		Complete(r)
}
