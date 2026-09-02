// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: SUSE LLC
// SPDX-FileCopyrightText: The Rancher Desktop Authors

package compose

import (
	"context"
	"testing"

	"gotest.tools/v3/assert"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/rancher-sandbox/rancher-desktop-daemon/pkg/apis/containers/v1alpha1"
)

// newValidComposeUpRequest returns a ComposeUpRequest whose metadata.name
// matches the deterministic name computed from namespace/name, so that only
// the specific field under test needs to be overridden by the caller.
func newValidComposeUpRequest(namespace, name string) *v1alpha1.ComposeUpRequest {
	return &v1alpha1.ComposeUpRequest{
		ObjectMeta: metav1.ObjectMeta{
			Name: generateProjectName(namespace, name),
		},
		Spec: v1alpha1.ComposeUpRequestSpec{
			Namespace: namespace,
			Name:      name,
		},
	}
}

func TestComposeUpRequestValidator_ValidateCreate(t *testing.T) {
	t.Parallel()

	v := &composeUpRequestValidator{}

	t.Run("accepts a correctly-named request", func(t *testing.T) {
		t.Parallel()
		request := newValidComposeUpRequest("moby", "myproject")
		_, err := v.ValidateCreate(context.Background(), request)
		assert.NilError(t, err)
	})

	t.Run("rejects a request whose metadata.name does not match the computed hash", func(t *testing.T) {
		t.Parallel()
		request := newValidComposeUpRequest("moby", "myproject")
		request.Name = "not-the-right-hash"
		_, err := v.ValidateCreate(context.Background(), request)
		assert.ErrorContains(t, err, "metadata.name must be")
	})
}
