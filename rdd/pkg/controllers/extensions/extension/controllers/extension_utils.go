// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: SUSE LLC
// SPDX-FileCopyrightText: The Rancher Desktop Authors

package controllers

import (
	"encoding/json"
	"fmt"

	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	containersv1alpha1 "github.com/rancher-sandbox/rancher-desktop-daemon/pkg/apis/containers/v1alpha1"
	"github.com/rancher-sandbox/rancher-desktop-daemon/pkg/apis/extensions/v1alpha1"
	"github.com/rancher-sandbox/rancher-desktop-daemon/pkg/util/api"
)

// getContainerNamespace returns the container engine namespace to use for the
// given extension.
func getContainerNamespace(_ *v1alpha1.Extension) string {
	// TODO: support namespaces, in containerd.
	return "moby"
}

// composeProjectNameForExtension returns the name of the Compose project for
// the given extension.
func composeProjectNameForExtension(ext *v1alpha1.Extension) string {
	return "rdx-" + ext.GetName()
}

// composeKeyForExtension returns the ObjectKey for the
// [v1alpha1.ComposeProject] and [v1alpha1.ComposeUpRequest] corresponding to
// the given extension.
func composeKeyForExtension(ext *v1alpha1.Extension) client.ObjectKey {
	return types.NamespacedName{
		Namespace: ext.Namespace,
		Name: api.MirrorName[*containersv1alpha1.ComposeProject](
			fmt.Sprintf("%s.%s", getContainerNamespace(ext), composeProjectNameForExtension(ext))),
	}
}

func getManifest(ext *v1alpha1.Extension) (*ExtensionManifest, error) {
	var manifest ExtensionManifest
	if ext.Status.Metadata == nil {
		return nil, nil
	}
	if err := json.Unmarshal(ext.Status.Metadata.Raw, &manifest); err != nil {
		return nil, fmt.Errorf("failed to unmarshal extension manifest: %w", err)
	}
	return &manifest, nil
}
