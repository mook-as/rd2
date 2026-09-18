// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: SUSE LLC
// SPDX-FileCopyrightText: The Rancher Desktop Authors

package controllers

import (
	"context"
	"errors"
	"fmt"

	"github.com/containerd/errdefs"

	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	containersv1alpha1 "github.com/rancher-sandbox/rancher-desktop-daemon/pkg/apis/containers/v1alpha1"
	containersv1alpha1apply "github.com/rancher-sandbox/rancher-desktop-daemon/pkg/apis/containers/v1alpha1/applyconfiguration/containers/v1alpha1"
	"github.com/rancher-sandbox/rancher-desktop-daemon/pkg/util/api"
)

// syncNamespaces lists containerd namespaces, creates or updates their
// ContainerNamespace mirrors, and prunes stale ones. Unlike Docker's static
// "moby" namespace, containerd namespaces come and go.
func (w *containerdWatcher) syncNamespaces(ctx context.Context) error {
	log := logf.FromContext(ctx).WithName("containerd-watcher")

	nsNames, err := w.cli.NamespaceService().List(ctx)
	if err != nil {
		return fmt.Errorf("failed to list namespaces: %w", err)
	}

	// Track which mirror names exist so stale mirrors can be pruned.
	activeNames := make(map[string]bool, len(nsNames))

	// Log and skip per-item Apply failures. A single permanently-broken
	// Apply must not pin ContainerEngineReady at ConnectFailed. Structural
	// errors below are still fatal.
	var errs []error
	for _, ns := range nsNames {
		if err := w.applyNamespace(ctx, ns); err != nil {
			log.Error(err, "Skipping namespace during full sync", "namespace", ns)
		}
		activeNames[api.MirrorName("cns", ns)] = true
	}

	// Remove stale ContainerNamespace mirrors.
	var nsMirrors containersv1alpha1.ContainerNamespaceList
	if err := w.k8s.List(ctx, &nsMirrors, client.InNamespace(w.apiNamespace)); err != nil {
		return fmt.Errorf("failed to list ContainerNamespaces: %w", err)
	}
	for i := range nsMirrors.Items {
		ns := &nsMirrors.Items[i]
		if !activeNames[ns.Name] {
			log.V(1).Info("Removing stale ContainerNamespace", "namespace", ns.Name)
			if err := w.removeMirrorResource(ctx, ns, ns.Name); err != nil {
				errs = append(errs, err)
			}
		}
	}

	return errors.Join(errs...)
}

// applyNamespace creates the ContainerNamespace mirror for a containerd
// namespace. This resource has no mirror finalizer: deleting the mirror is
// not offered as a way to delete the containerd namespace, and
// cleanupMirrorResources sweeps it unconditionally on VM stop, so a
// finalizer with no handler would only trap user deletes in Terminating.
//
// A K8s-side delete therefore stays gone. Only fullSync and the container,
// and image create events, or various namespace change events call this, so
// nothing re-applies the mirror until one of those fires again or the watcher
// restarts.
func (w *containerdWatcher) applyNamespace(ctx context.Context, ns string) error {
	applyConfig := containersv1alpha1apply.ContainerNamespace(
		api.MirrorName("cns", ns),
		w.apiNamespace)

	err := w.k8s.Apply(ctx, applyConfig,
		client.ForceOwnership, client.FieldOwner(controllerName))
	if err != nil {
		return err
	}

	// Apply the name and the labels with separate field owners; this ensures that
	// if we failed to get the labels, we do not overwrite the existing ones.
	applyConfig.WithStatus(containersv1alpha1apply.ContainerNamespaceStatus().
		WithName(ns))
	err = w.k8s.Status().Apply(ctx, applyConfig,
		client.ForceOwnership, client.FieldOwner(controllerName))
	if err != nil {
		return err
	}

	labels, err := w.cli.NamespaceService().Labels(ctx, ns)
	if err != nil {
		// Do not log if the namespace disappeared: we'll handle a delete event in
		// the future.
		if !errdefs.IsNotFound(err) {
			logf.FromContext(ctx).WithName("containerd-watcher").
				Error(err, "Failed to get labels for namespace", "namespace", ns)
		}
		return nil
	}

	applyConfig.WithStatus(containersv1alpha1apply.ContainerNamespaceStatus().
		WithLabels(labels))
	return w.k8s.Status().Apply(ctx, applyConfig,
		client.ForceOwnership, client.FieldOwner(controllerName+"-labels"))
}

// removeNamespace deletes the ContainerNamespace mirror for a containerd
// namespace that is gone.
func (w *containerdWatcher) removeNamespace(ctx context.Context, ns string) error {
	return w.removeMirrorResource(
		ctx,
		&containersv1alpha1.ContainerNamespace{},
		api.MirrorName("cns", ns))
}
