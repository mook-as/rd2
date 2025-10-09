// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: SUSE LLC
// SPDX-FileCopyrightText: The Rancher Desktop Authors

package limavm

import (
	"context"
	"errors"
	"fmt"

	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/rancher-sandbox/rancher-desktop-daemon/pkg/apis/lima/v1alpha1"
)

// ValidateLimaVMUniqueName validates that the LimaVM name is unique across all namespaces.
// This is critical because LimaVM names correspond to actual VM instances on the host system,
// which must be unique.
func ValidateLimaVMUniqueName(ctx context.Context, c client.Client, limavm *v1alpha1.LimaVM) error {
	if limavm == nil {
		return errors.New("limavm object cannot be nil")
	}

	// List all LimaVMs across all namespaces
	limavmList := &v1alpha1.LimaVMList{}
	if err := c.List(ctx, limavmList, &client.ListOptions{}); err != nil {
		return fmt.Errorf("failed to list LimaVMs for uniqueness check: %w", err)
	}

	// Check for name conflicts in other namespaces
	for _, existingVM := range limavmList.Items {
		// Skip if it's an update to the existing resource (same namespace and name)
		if existingVM.Namespace == limavm.Namespace && existingVM.Name == limavm.Name {
			continue
		}

		// Check if an instance with the same name exists in a different namespace
		if existingVM.Name == limavm.Name {
			return fmt.Errorf("LimaVM name %q is already used in namespace %q; LimaVM names must be unique across all namespaces",
				limavm.Name, existingVM.Namespace)
		}
	}

	return nil
}

// ValidateLimaVM validates a complete LimaVM object and returns warnings.
func ValidateLimaVM(ctx context.Context, c client.Client, limavm *v1alpha1.LimaVM) ([]string, error) {
	if limavm == nil {
		return nil, errors.New("limavm object cannot be nil")
	}

	var warnings []string
	if err := ValidateLimaVMUniqueName(ctx, c, limavm); err != nil {
		return warnings, err
	}

	return warnings, nil
}
