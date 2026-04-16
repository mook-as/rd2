// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: SUSE LLC
// SPDX-FileCopyrightText: The Rancher Desktop Authors

package container

import (
	"context"
	"errors"
	"fmt"

	"k8s.io/apimachinery/pkg/api/equality"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrlwebhookadmission "sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/rancher-sandbox/rancher-desktop-daemon/pkg/apis/containers/v1alpha1"
)

type immutableValidator struct {
	Client client.Client
}

// ValidateCreate implements [ctrlwebhookadmission.Validator].
func (c *immutableValidator) ValidateCreate(context.Context, *v1alpha1.Container) (warnings ctrlwebhookadmission.Warnings, err error) {
	return nil, errors.New("webhook does not implement create")
}

// ValidateDelete implements [ctrlwebhookadmission.Validator].
func (c *immutableValidator) ValidateDelete(context.Context, *v1alpha1.Container) (warnings ctrlwebhookadmission.Warnings, err error) {
	return nil, errors.New("webhook does not implement delete")
}

// ValidateUpdate implements [ctrlwebhookadmission.Validator].
// Only spec.state changes are allowed; all other spec fields are immutable.
// The CRD schema already constrains spec.state to the valid enum
// ({created, running, unknown}), so this webhook only enforces that
// nothing else in the spec changes.
func (c *immutableValidator) ValidateUpdate(_ context.Context, oldContainer, newContainer *v1alpha1.Container) (warnings ctrlwebhookadmission.Warnings, err error) {
	oldCopy := oldContainer.Spec
	oldCopy.State = newContainer.Spec.State
	if !equality.Semantic.DeepEqual(oldCopy, newContainer.Spec) {
		return nil, fmt.Errorf("only spec.state may be changed: old: %v, new: %v", oldContainer.Spec, newContainer.Spec)
	}
	return nil, nil
}
