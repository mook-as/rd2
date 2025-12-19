// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: SUSE LLC
// SPDX-FileCopyrightText: The Rancher Desktop Authors

package container

import (
	"context"
	"errors"
	"fmt"

	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrlwebhookadmission "sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/rancher-sandbox/rancher-desktop-daemon/pkg/apis/containers/v1alpha1"
)

type ContainerImmutableValidator struct {
	Client client.Client
}

// ValidateCreate implements ctrlwebhookadmission.CustomValidator.
func (c *ContainerImmutableValidator) ValidateCreate(_ context.Context, _ runtime.Object) (warnings ctrlwebhookadmission.Warnings, err error) {
	return nil, errors.New("webhook does not implement create")
}

// ValidateDelete implements ctrlwebhookadmission.CustomValidator.
func (c *ContainerImmutableValidator) ValidateDelete(_ context.Context, _ runtime.Object) (warnings ctrlwebhookadmission.Warnings, err error) {
	return nil, errors.New("webhook does not implement delete")
}

// ValidateUpdate implements ctrlwebhookadmission.CustomValidator.
func (c *ContainerImmutableValidator) ValidateUpdate(_ context.Context, oldObj, newObj runtime.Object) (warnings ctrlwebhookadmission.Warnings, err error) {
	// Return an error if the old object does not match the new object.
	oldContainer, ok := oldObj.(*v1alpha1.Container)
	if !ok {
		return nil, fmt.Errorf("old object should be a Container, but got %T", oldObj)
	}
	newContainer, ok := newObj.(*v1alpha1.Container)
	if !ok {
		return nil, fmt.Errorf("new object should be a Container, but got a %T", newObj)
	}

	if !equality.Semantic.DeepEqual(oldContainer.Spec, newContainer.Spec) {
		return nil, fmt.Errorf("container objects must not be modified: old: %v, new: %v", oldContainer, newContainer)
	}

	return nil, nil
}
