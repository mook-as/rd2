// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: SUSE LLC
// SPDX-FileCopyrightText: The Rancher Desktop Authors

package compose

import (
	"context"
	"fmt"

	ctrlwebhookadmission "sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/rancher-sandbox/rancher-desktop-daemon/pkg/apis/containers/v1alpha1"
)

// composeUpRequestValidator enforces the invariant for ComposeUpRequest that
// cannot be expressed declaratively in the CRD schema: metadata.name must be
// calculated from spec.namespace and spec.name (matching the name of the
// Compose object it will create or update).
//
// Immutability of spec.namespace, spec.name, spec.workingDir, and
// spec.configs is enforced declaratively via CEL
// (`+kubebuilder:validation:XValidation`) on ComposeUpRequestSpec, so it is
// not duplicated here.
type composeUpRequestValidator struct{}

// ValidateCreate implements [ctrlwebhookadmission.Validator].
func (v *composeUpRequestValidator) ValidateCreate(_ context.Context, request *v1alpha1.ComposeUpRequest) (warnings ctrlwebhookadmission.Warnings, err error) {
	expectedName := generateProjectName(request.Spec.Namespace, request.Spec.Name)
	if request.Name != expectedName {
		return nil, fmt.Errorf(
			"metadata.name must be %q (sha256 of spec.namespace/spec.name), got %q",
			expectedName, request.Name)
	}
	return nil, nil
}

// ValidateUpdate implements [ctrlwebhookadmission.Validator].
func (v *composeUpRequestValidator) ValidateUpdate(_ context.Context, _, _ *v1alpha1.ComposeUpRequest) (warnings ctrlwebhookadmission.Warnings, err error) {
	// metadata.name is immutable on update (it is only ever set on create), and
	// the rest of spec is immutable via CEL, so there is nothing left to check.
	return nil, nil
}

// ValidateDelete implements [ctrlwebhookadmission.Validator].
func (v *composeUpRequestValidator) ValidateDelete(_ context.Context, _ *v1alpha1.ComposeUpRequest) (warnings ctrlwebhookadmission.Warnings, err error) {
	// We do not do any validation on delete.
	return nil, nil
}

// Compose objects are, by convention, only ever created/updated by this
// package's own reconciler; this is not enforced by a validator, since every
// local client (including the reconciler itself) authenticates with the same
// admin-equivalent identity on this single-user desktop , so a group-based
// identity check would not distinguish the two.
