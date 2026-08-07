// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: SUSE LLC
// SPDX-FileCopyrightText: The Rancher Desktop Authors

package controllers

import (
	"context"
	"errors"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	containersv1alpha1 "github.com/rancher-sandbox/rancher-desktop-daemon/pkg/apis/containers/v1alpha1"
	"github.com/rancher-sandbox/rancher-desktop-daemon/pkg/apis/extensions/v1alpha1"
	"github.com/rancher-sandbox/rancher-desktop-daemon/pkg/controllers/base"
)

// ExtensionStartedReconciler reconciles an Extension object's Started
// condition: starting the extension's containers (if any) once installed,
// and stopping them again on deletion.
// This assumes that the Extension object has a cleanup finalizer managed by the
// Installed condition reconciler, therefore this would not need to block the
// object from being deleted.
type ExtensionStartedReconciler struct {
	client.Client
}

var _ reconcile.Reconciler = &ExtensionStartedReconciler{}

// +kubebuilder:rbac:groups=extensions.rancherdesktop.io,resources=extensions,verbs=get;list;watch
// +kubebuilder:rbac:groups=extensions.rancherdesktop.io,resources=extensions/status,verbs=get;update;patch

// Reconcile the Extension resource's Started condition.
func (r *ExtensionStartedReconciler) Reconcile(ctx context.Context, req reconcile.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	log.V(1).Info("Reconciling Extension Started condition",
		"name", req.Name, "namespace", req.Namespace)

	extKey := client.ObjectKey{Namespace: req.Namespace, Name: req.Name}
	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		var ext v1alpha1.Extension
		if err := r.Get(ctx, extKey, &ext); err != nil {
			return client.IgnoreNotFound(err)
		}
		return r.reconcile(ctx, &ext)
	})
	return ctrl.Result{}, err
}

// reconcile computes and applies the Started condition for a freshly-fetched
// ext.  It is called from within the RetryOnConflict loop in Reconcile.
func (r *ExtensionStartedReconciler) reconcile(ctx context.Context, ext *v1alpha1.Extension) error {
	if base.IsBeingDeleted(ext) {
		return r.reconcileDelete(ctx, ext)
	}

	// Don't bother doing anything until we're installed.
	if !apimeta.IsStatusConditionTrue(ext.Status.Conditions, v1alpha1.ExtensionConditionInstalled) {
		return r.reconcileNotInstalled(ctx, ext)
	}

	// Check if we actually have anything to start
	manifest, err := getManifest(ext)
	if err != nil {
		logf.FromContext(ctx).Error(err, "Failed to get extension manifest")
		return err
	}
	if manifest == nil {
		logf.FromContext(ctx).Error(nil, "Extension metadata is not available")
		return errors.New("extension metadata is not available")
	}
	if manifest.VM.Image == "" && manifest.VM.ComposeFile == "" {
		// Nothing to start.
		if apimeta.SetStatusCondition(&ext.Status.Conditions, metav1.Condition{
			Type:    v1alpha1.ExtensionConditionStarted,
			Status:  metav1.ConditionTrue,
			Reason:  v1alpha1.ExtensionStartedReasonStarted,
			Message: "Extension has nothing to start",
		}) {
			return r.Status().Update(ctx, ext)
		}
		return nil
	}

	var compose containersv1alpha1.ComposeProject
	err = r.Get(ctx, composeKeyForExtension(ext), &compose)
	if client.IgnoreNotFound(err) != nil {
		logf.FromContext(ctx).Error(err, "Failed to get Compose object for Extension")
		return err
	}

	if err != nil {
		// Compose object does not exist.
		return r.reconcileComposeMissing(ctx, ext, composeProjectNameForExtension(ext))
	}

	// Compose object exists; make sure we have a owner reference on it.
	hasOwnerRef, err := controllerutil.HasOwnerReference(compose.GetOwnerReferences(), ext, r.Scheme())
	if err != nil {
		return err
	}
	if !hasOwnerRef {
		err = controllerutil.SetOwnerReference(
			ext,
			&compose,
			r.Scheme(),
			controllerutil.WithBlockOwnerDeletion(true))
		if err != nil {
			logf.FromContext(ctx).Error(err, "Failed to set owner reference on Compose object")
			return err
		}
		if err := r.Update(ctx, &compose); err != nil {
			return err
		}
	}
	// Everything is good; set the Started condition to true.
	apimeta.SetStatusCondition(&ext.Status.Conditions, metav1.Condition{
		Type:    v1alpha1.ExtensionConditionStarted,
		Status:  metav1.ConditionTrue,
		Reason:  v1alpha1.ExtensionStartedReasonStarted,
		Message: "Extension has started",
	})
	return r.Status().Update(ctx, ext)
}

// reconcileDelete handles the case where ext is being deleted: it ensures that
// the corresponding Compose / ComposeUpRequest objects are also deleted.
func (r *ExtensionStartedReconciler) reconcileDelete(ctx context.Context, ext *v1alpha1.Extension) error {
	setStatusCondition := apimeta.SetStatusCondition(&ext.Status.Conditions, metav1.Condition{
		Type:    v1alpha1.ExtensionConditionStarted,
		Status:  metav1.ConditionFalse,
		Reason:  v1alpha1.ExtensionStartedReasonStopping,
		Message: "Extension is stopping",
	})

	var req containersv1alpha1.ComposeUpRequest
	composeKey := composeKeyForExtension(ext)
	err := r.Get(ctx, composeKey, &req)
	if client.IgnoreNotFound(err) != nil {
		return err
	} else if err == nil {
		// Update the status, before we attempt the actual delete.
		if setStatusCondition {
			if err := r.Status().Update(ctx, ext); err != nil {
				return client.IgnoreNotFound(err)
			}
		}
		return client.IgnoreNotFound(r.Delete(ctx, &req))
	}

	var compose containersv1alpha1.ComposeProject
	err = r.Get(ctx, composeKey, &compose)
	if client.IgnoreNotFound(err) != nil {
		return err
	} else if err == nil {
		if setStatusCondition {
			// Update the status, before we attempt the actual delete.
			if err := r.Status().Update(ctx, ext); err != nil {
				return client.IgnoreNotFound(err)
			}
		}
		return client.IgnoreNotFound(r.Delete(ctx, &compose))
	}

	// Both Compose and ComposeUpRequest are gone; set the status to Stopped.
	changed := apimeta.SetStatusCondition(&ext.Status.Conditions, metav1.Condition{
		Type:    v1alpha1.ExtensionConditionStarted,
		Status:  metav1.ConditionFalse,
		Reason:  v1alpha1.ExtensionStartedReasonStopped,
		Message: "Extension has stopped",
	})
	if changed {
		return client.IgnoreNotFound(r.Status().Update(ctx, ext))
	}

	return nil
}

// reconcileNotInstalled sets the Started condition to reflect that the
// extension is not yet installed, so it cannot be started.
func (r *ExtensionStartedReconciler) reconcileNotInstalled(ctx context.Context, ext *v1alpha1.Extension) error {
	apimeta.SetStatusCondition(&ext.Status.Conditions, metav1.Condition{
		Type:    v1alpha1.ExtensionConditionStarted,
		Status:  metav1.ConditionFalse,
		Reason:  v1alpha1.ExtensionStartedReasonInstalling,
		Message: "Extension is not installed, so it cannot be started",
	})
	return client.IgnoreNotFound(r.Status().Update(ctx, ext))
}

// reconcileComposeMissing handles the case where the Compose object does not
// exist yet: it looks up (or creates) the corresponding ComposeUpRequest and
// reflects its progress via the Started condition.
func (r *ExtensionStartedReconciler) reconcileComposeMissing(ctx context.Context, ext *v1alpha1.Extension, projectName string) error {
	log := logf.FromContext(ctx)

	var req containersv1alpha1.ComposeUpRequest
	err := r.Get(ctx, composeKeyForExtension(ext), &req)
	if apierrors.IsNotFound(err) {
		return r.createComposeUpRequest(ctx, ext, projectName)
	} else if err != nil {
		log.Error(err, "Failed to get ComposeUpRequest for Extension")
		return err
	}

	// The request already exists.
	settled := apimeta.FindStatusCondition(req.Status.Conditions, containersv1alpha1.ComposeUpRequestConditionSettled)
	if settled == nil || settled.Status != metav1.ConditionTrue {
		// The request already exists, but is not settled; we're still starting.
		changed := apimeta.SetStatusCondition(&ext.Status.Conditions, metav1.Condition{
			Type:    v1alpha1.ExtensionConditionStarted,
			Status:  metav1.ConditionFalse,
			Reason:  v1alpha1.ExtensionStartedReasonStarting,
			Message: "Extension is starting",
		})
		if changed {
			return client.IgnoreNotFound(r.Status().Update(ctx, ext))
		}
		return nil
	}
	// The request has settled; see if it succeeded.
	if settled.Reason == containersv1alpha1.ComposeUpRequestSettledReasonSucceeded {
		apimeta.SetStatusCondition(&ext.Status.Conditions, metav1.Condition{
			Type:    v1alpha1.ExtensionConditionStarted,
			Status:  metav1.ConditionTrue,
			Reason:  v1alpha1.ExtensionStartedReasonStarted,
			Message: "Extension has started",
		})
	} else {
		apimeta.SetStatusCondition(&ext.Status.Conditions, metav1.Condition{
			Type:    v1alpha1.ExtensionConditionStarted,
			Status:  metav1.ConditionFalse,
			Reason:  v1alpha1.ExtensionStartedReasonStartFailed,
			Message: fmt.Sprintf("Extension failed to start: %s", settled.Message),
		})
	}
	return client.IgnoreNotFound(r.Status().Update(ctx, ext))
}

// createComposeUpRequest creates a new ComposeUpRequest to start ext's
// containers.  This may be because the request failed and got reaped, in which
// case we are going to retry.
func (r *ExtensionStartedReconciler) createComposeUpRequest(ctx context.Context, ext *v1alpha1.Extension, projectName string) error {
	log := logf.FromContext(ctx)

	extensionDir, err := extensionInstallDir(ext)
	if err != nil {
		log.Error(err, "Failed to get extension install directory")
		return err
	}
	composeKey := composeKeyForExtension(ext)
	req := containersv1alpha1.ComposeUpRequest{
		Name:      composeKey.Name,
		Namespace: composeKey.Namespace,
		Spec: containersv1alpha1.ComposeUpRequestSpec{
			Namespace:  getContainerNamespace(ext),
			Name:       projectName,
			WorkingDir: extensionDir,
		},
	}
	err = controllerutil.SetOwnerReference(
		ext,
		&req,
		r.Scheme(),
		controllerutil.WithBlockOwnerDeletion(true))
	if err != nil {
		return err
	}
	if err := r.Create(ctx, &req); err != nil {
		log.Error(err, "Failed to create ComposeUpRequest for Extension")
		return err
	}
	apimeta.SetStatusCondition(&ext.Status.Conditions, metav1.Condition{
		Type:    v1alpha1.ExtensionConditionStarted,
		Status:  metav1.ConditionFalse,
		Reason:  v1alpha1.ExtensionStartedReasonStarting,
		Message: "Extension is starting",
	})
	return r.Status().Update(ctx, ext)
}

// SetupWithManager sets up the controller with the Manager.
func (r *ExtensionStartedReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.Extension{}).
		Owns(&containersv1alpha1.ComposeProject{}, builder.MatchEveryOwner).
		Owns(&containersv1alpha1.ComposeUpRequest{}, builder.MatchEveryOwner).
		Named("extension-started-reconciler").
		Complete(r)
}
