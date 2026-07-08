// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: SUSE LLC
// SPDX-FileCopyrightText: The Rancher Desktop Authors

// Package controllers implements the HostInfo reconciler, which maintains a
// cluster-scoped singleton that exposes host hardware limits in its Status.
package controllers

import (
	"context"
	goruntime "runtime"

	"github.com/pbnjay/memory"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	rddv1alpha1 "github.com/rancher-sandbox/rancher-desktop-daemon/pkg/apis/rdd/v1alpha1"
)

// SingletonName is the fixed name of the HostInfo singleton.
const SingletonName = "system"

// HostInfoReconciler reconciles the HostInfo singleton.
type HostInfoReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=rdd.rancherdesktop.io,resources=hostinfos,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=rdd.rancherdesktop.io,resources=hostinfos/status,verbs=get;update;patch

// Reconcile reads the host hardware limits and writes them to HostInfo.Status.
func (r *HostInfoReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	var hi rddv1alpha1.HostInfo
	if err := r.Get(ctx, req.NamespacedName, &hi); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	cpus := goruntime.NumCPU()
	mem := int64(memory.TotalMemory())

	patch := client.MergeFrom(hi.DeepCopy())
	hi.Status.CPUs = cpus
	hi.Status.Memory = mem

	if err := r.Status().Patch(ctx, &hi, patch); err != nil {
		log.Error(err, "Failed to update HostInfo status")
		return ctrl.Result{}, err
	}
	log.Info("Updated HostInfo status", "cpus", cpus, "memory", mem)
	return ctrl.Result{}, nil
}

// Start implements manager.Runnable. It bootstraps the HostInfo singleton
// once the cache is ready so that the reconciler loop can populate its Status.
// Only the leader runs this to avoid concurrent creates.
func (r *HostInfoReconciler) Start(ctx context.Context) error {
	hi := &rddv1alpha1.HostInfo{
		ObjectMeta: metav1.ObjectMeta{Name: SingletonName},
	}
	if err := r.Create(ctx, hi); err != nil && !apierrors.IsAlreadyExists(err) {
		// Non-fatal: log and continue. The next reconcile will retry.
		logf.FromContext(ctx).Error(err, "Failed to bootstrap HostInfo singleton")
	}
	return nil
}

// NeedLeaderElection implements manager.LeaderElectionRunnable so that only
// the leader creates the HostInfo singleton.
func (r *HostInfoReconciler) NeedLeaderElection() bool { return true }

// SetupWithManager sets up the controller with the Manager.
func (r *HostInfoReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if err := mgr.Add(r); err != nil {
		return err
	}
	return ctrl.NewControllerManagedBy(mgr).
		For(&rddv1alpha1.HostInfo{}).
		Complete(r)
}
