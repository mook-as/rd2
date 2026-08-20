// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: SUSE LLC
// SPDX-FileCopyrightText: The Rancher Desktop Authors

package composeproject

import (
	"context"
	"errors"
	"fmt"

	ctrlwebhookadmission "sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/rancher-sandbox/rancher-desktop-daemon/pkg/apis/containers/v1alpha1"
)

// composeProjectValidator enforces the invariants for ComposeProject that
// cannot be expressed declaratively in the CRD schema:
//   - metadata.name must be calculated from spec.namespace and spec.name.
//   - the action annotation, when present, must carry a valid
//     v1alpha1.ComposeProjectAction; annotations are untyped, so the CRD
//     schema cannot validate them.
//
// Immutability of spec.namespace and spec.name is enforced declaratively via
// CEL (`+kubebuilder:validation:XValidation`) on ComposeProjectSpec, so it is
// not duplicated here.
type composeProjectValidator struct{}

// ValidateCreate implements [ctrlwebhookadmission.Validator].
func (v *composeProjectValidator) ValidateCreate(_ context.Context, project *v1alpha1.ComposeProject) (warnings ctrlwebhookadmission.Warnings, err error) {
	var errs []error
	expectedName := generateProjectName(project.Spec.Namespace, project.Spec.Name)
	if project.Name != expectedName {
		errs = append(errs, fmt.Errorf(
			"metadata.name must be %q (sha256 of spec.namespace/spec.name), got %q",
			expectedName, project.Name))
	}
	errs = append(errs, v.validateActionAnnotation(project))
	return nil, errors.Join(errs...)
}

// ValidateUpdate implements [ctrlwebhookadmission.Validator].
func (v *composeProjectValidator) ValidateUpdate(_ context.Context, _, newProject *v1alpha1.ComposeProject) (warnings ctrlwebhookadmission.Warnings, err error) {
	return nil, v.validateActionAnnotation(newProject)
}

// ValidateDelete implements [ctrlwebhookadmission.Validator].
func (v *composeProjectValidator) ValidateDelete(_ context.Context, _ *v1alpha1.ComposeProject) (warnings ctrlwebhookadmission.Warnings, err error) {
	// We do not do any validation on delete.
	return nil, nil
}

// validateActionAnnotation checks that the action annotation, if present, is
// a recognized ComposeProjectAction.
func (v *composeProjectValidator) validateActionAnnotation(project *v1alpha1.ComposeProject) error {
	if raw, ok := project.Annotations[v1alpha1.AnnotationAction]; ok {
		if !v1alpha1.ComposeProjectAction(raw).IsValid() {
			return fmt.Errorf("invalid %s annotation %q", v1alpha1.AnnotationAction, raw)
		}
		if raw == string(v1alpha1.ComposeProjectActionUp) && project.Spec.WorkingDir == "" {
			return fmt.Errorf("%s annotation %q requires spec.workingDir to be set", v1alpha1.AnnotationAction, raw)
		}
	}
	return nil
}
