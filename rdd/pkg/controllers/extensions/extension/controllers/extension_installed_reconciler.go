// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: SUSE LLC
// SPDX-FileCopyrightText: The Rancher Desktop Authors

package controllers

import (
	"context"
	"fmt"
	"time"

	"github.com/distribution/reference"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/rancher-sandbox/rancher-desktop-daemon/pkg/apis/extensions/v1alpha1"
	"github.com/rancher-sandbox/rancher-desktop-daemon/pkg/controllers/base"
)

// installedFinalizer is added to Extension resources so uninstall can run
// the pre-uninstall script and delete extracted files before the resource is
// actually removed.
const installedFinalizer = "extensions.rancherdesktop.io/installed"

// ExtensionInstalledReconciler reconciles an Extension object's Installed
// condition: downloading and extracting the image, running its post-install
// script, and (on deletion) running the pre-uninstall script and cleaning up
// extracted files.
type ExtensionInstalledReconciler struct {
	client.Client
}

var _ reconcile.ObjectReconciler[*v1alpha1.Extension] = &ExtensionInstalledReconciler{}

// +kubebuilder:rbac:groups=extensions.rancherdesktop.io,resources=extensions,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=extensions.rancherdesktop.io,resources=extensions/status,verbs=get;update;patch

// Reconcile the Extension resource's Installed condition.
func (r *ExtensionInstalledReconciler) Reconcile(ctx context.Context, ext *v1alpha1.Extension) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	log.V(1).Info("Reconciling Extension Installed condition",
		"name", ext.Name, "namespace", ext.Namespace)

	if base.IsBeingDeleted(ext) {
		return r.reconcileDelete(ctx, ext)
	}

	if !controllerutil.ContainsFinalizer(ext, installedFinalizer) {
		return ctrl.Result{}, r.addFinalizer(ctx, ext)
	}

	installed := apimeta.FindStatusCondition(ext.Status.Conditions, v1alpha1.ExtensionConditionInstalled)
	switch {
	case installed == nil, installed.Reason == v1alpha1.ExtensionInstalledReasonResolving:
		return ctrl.Result{}, r.resolveImage(ctx, ext)

	case installed.Reason == v1alpha1.ExtensionInstalledReasonDownloading:
		// TODO: download the image (r.download), then set Downloading or
		// DownloadFailed; requeue on success to progress to Extracting.
		return ctrl.Result{}, r.setInstalledCondition(ctx, ext,
			metav1.ConditionFalse, v1alpha1.ExtensionInstalledReasonDownloading,
			"Downloading extension image")

	case installed.Reason == v1alpha1.ExtensionInstalledReasonExtracting:
		// TODO: extract the downloaded image (r.extract), then set
		// PostInstallRunning or ExtractFailed.
		return ctrl.Result{}, nil

	case installed.Reason == v1alpha1.ExtensionInstalledReasonPostInstallRunning:
		// TODO: run the extension's post-install script (r.runPostInstall),
		// then set Installed or PostInstallFailed.
		return ctrl.Result{}, nil

	default:
		// Installed, or a terminal failure; nothing more to do here.
		return ctrl.Result{}, nil
	}
}

// resolveImage resolves ext.Spec.Image into status.image: if the image
// reference has no tag, it defaults to "latest" (real tag discovery, e.g.
// picking the highest semver tag, is not yet implemented). On success it
// sets status.image and advances the Installed condition to Downloading; on
// an invalid image reference it sets ResolveFailed (terminal).
func (r *ExtensionInstalledReconciler) resolveImage(ctx context.Context, ext *v1alpha1.Extension) error {
	named, err := reference.ParseNormalizedNamed(ext.Spec.Image)
	if err != nil {
		return r.setInstalledCondition(ctx, ext,
			metav1.ConditionFalse, v1alpha1.ExtensionInstalledReasonResolveFailed,
			fmt.Sprintf("invalid image reference %q: %v", ext.Spec.Image, err))
	}
	resolved := reference.TagNameOnly(named).String()

	key := client.ObjectKeyFromObject(ext)
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		latest := &v1alpha1.Extension{}
		if err := r.Get(ctx, key, latest); err != nil {
			return client.IgnoreNotFound(err)
		}
		latest.Status.Image = resolved
		apimeta.SetStatusCondition(&latest.Status.Conditions, metav1.Condition{
			Type:               v1alpha1.ExtensionConditionInstalled,
			Status:             metav1.ConditionFalse,
			ObservedGeneration: latest.Generation,
			Reason:             v1alpha1.ExtensionInstalledReasonDownloading,
			Message:            "Downloading extension image",
		})
		return r.Status().Update(ctx, latest)
	})
}

// reconcileDelete handles uninstall: it runs the pre-uninstall script and
// deletes extracted files (as tracked by the Installed condition's reason),
// then removes installedFinalizer once cleanup has finished.
func (r *ExtensionInstalledReconciler) reconcileDelete(ctx context.Context, ext *v1alpha1.Extension) (ctrl.Result, error) {
	if !controllerutil.ContainsFinalizer(ext, installedFinalizer) {
		return ctrl.Result{}, nil
	}

	installed := apimeta.FindStatusCondition(ext.Status.Conditions, v1alpha1.ExtensionConditionInstalled)
	if installed == nil || installed.Reason != v1alpha1.ExtensionInstalledReasonUninstalled {
		reason := v1alpha1.ExtensionInstalledReasonPreUninstallRunning
		if installed != nil && installed.Reason == v1alpha1.ExtensionInstalledReasonDeleting {
			reason = v1alpha1.ExtensionInstalledReasonDeleting
		}
		// TODO: run the pre-uninstall script (ignoring failures, per the
		// design doc) then delete extracted files (r.deleteFiles); set
		// Uninstalled or DeleteFailed on completion.
		return ctrl.Result{}, r.setInstalledCondition(ctx, ext,
			metav1.ConditionFalse, reason, "Removing extension")
	}

	return ctrl.Result{}, r.removeFinalizer(ctx, ext)
}

// addFinalizer adds installedFinalizer to ext, retrying on conflict against
// a freshly-fetched copy since other reconcilers may concurrently update it.
func (r *ExtensionInstalledReconciler) addFinalizer(ctx context.Context, ext *v1alpha1.Extension) error {
	key := client.ObjectKeyFromObject(ext)
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		latest := &v1alpha1.Extension{}
		if err := r.Get(ctx, key, latest); err != nil {
			return client.IgnoreNotFound(err)
		}
		if !controllerutil.AddFinalizer(latest, installedFinalizer) {
			return nil
		}
		return r.Update(ctx, latest)
	})
}

// removeFinalizer removes installedFinalizer from ext, retrying on conflict
// against a freshly-fetched copy.
func (r *ExtensionInstalledReconciler) removeFinalizer(ctx context.Context, ext *v1alpha1.Extension) error {
	key := client.ObjectKeyFromObject(ext)
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		latest := &v1alpha1.Extension{}
		if err := r.Get(ctx, key, latest); err != nil {
			if apierrors.IsNotFound(err) {
				return nil
			}
			return err
		}
		if !controllerutil.RemoveFinalizer(latest, installedFinalizer) {
			return nil
		}
		return r.Update(ctx, latest)
	})
}

// setInstalledCondition sets the Installed condition on a freshly-fetched
// copy of ext, retrying on conflict.
func (r *ExtensionInstalledReconciler) setInstalledCondition(ctx context.Context, ext *v1alpha1.Extension, status metav1.ConditionStatus, reason, message string) error {
	key := client.ObjectKeyFromObject(ext)
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		latest := &v1alpha1.Extension{}
		if err := r.Get(ctx, key, latest); err != nil {
			return client.IgnoreNotFound(err)
		}
		changed := apimeta.SetStatusCondition(&latest.Status.Conditions, metav1.Condition{
			Type:               v1alpha1.ExtensionConditionInstalled,
			Status:             status,
			ObservedGeneration: latest.Generation,
			Reason:             reason,
			Message:            message,
		})
		if !changed {
			return nil
		}
		return r.Status().Update(ctx, latest)
	})
}

// SetupWithManager sets up the controller with the Manager.
func (r *ExtensionInstalledReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.Extension{}).
		Named("extension-installed-reconciler").
		Complete(reconcile.AsReconciler(mgr.GetClient(), r))
}
