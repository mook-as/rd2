// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: SUSE LLC
// SPDX-FileCopyrightText: The Rancher Desktop Authors

package controllers

import (
	"context"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/rancher-sandbox/rancher-desktop-daemon/pkg/apis/extensions/v1alpha1"
)

// ExtensionStartedReconciler reconciles an Extension object's Started
// condition: starting the extension's containers (if any) once installed,
// and stopping them again on deletion.
type ExtensionStartedReconciler struct {
	client.Client
}

var _ reconcile.ObjectReconciler[*v1alpha1.Extension] = &ExtensionStartedReconciler{}

// +kubebuilder:rbac:groups=extensions.rancherdesktop.io,resources=extensions,verbs=get;list;watch
// +kubebuilder:rbac:groups=extensions.rancherdesktop.io,resources=extensions/status,verbs=get;update;patch

// Reconcile the Extension resource's Started condition.
func (r *ExtensionStartedReconciler) Reconcile(ctx context.Context, ext *v1alpha1.Extension) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	log.V(1).Info("Reconciling Extension Started condition",
		"name", ext.Name, "namespace", ext.Namespace)

	// TODO: this is currently stubbed out (it doesn't do anything yet);
	// nothing produces the Started condition. Once implemented, this
	// should, once the Installed condition is True, start the extension's
	// containers (if any) and set Started or StartFailed; and, on
	// deletion, stop them again (ignoring failures, per the design doc)
	// before setting Stopping.
	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ExtensionStartedReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.Extension{}).
		Named("extension-started-reconciler").
		Complete(reconcile.AsReconciler(mgr.GetClient(), r))
}
