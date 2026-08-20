// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: SUSE LLC
// SPDX-FileCopyrightText: The Rancher Desktop Authors

package composeproject

import (
	"context"
	"testing"

	"gotest.tools/v3/assert"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/rancher-sandbox/rancher-desktop-daemon/pkg/apis/containers/v1alpha1"
)

// newValidComposeProject returns a ComposeProject whose metadata.name matches
// the deterministic name computed from namespace/name, so that only the
// specific field under test needs to be overridden by the caller.
func newValidComposeProject(namespace, name string) *v1alpha1.ComposeProject {
	return &v1alpha1.ComposeProject{
		ObjectMeta: metav1.ObjectMeta{
			Name: generateProjectName(namespace, name),
		},
		Spec: v1alpha1.ComposeProjectSpec{
			Namespace: namespace,
			Name:      name,
		},
	}
}

func TestComposeProjectValidator_ValidateCreate(t *testing.T) {
	t.Parallel()

	v := &composeProjectValidator{}

	t.Run("accepts a correctly-named project with no annotation", func(t *testing.T) {
		t.Parallel()
		project := newValidComposeProject("moby", "myproject")
		_, err := v.ValidateCreate(context.Background(), project)
		assert.NilError(t, err)
	})

	t.Run("accepts a correctly-named project with a valid action annotation", func(t *testing.T) {
		t.Parallel()
		project := newValidComposeProject("moby", "myproject")
		project.Annotations = map[string]string{
			v1alpha1.AnnotationAction: string(v1alpha1.ComposeProjectActionUp),
		}
		project.Spec.WorkingDir = t.TempDir()
		_, err := v.ValidateCreate(context.Background(), project)
		assert.NilError(t, err)
	})

	t.Run("rejects a project whose metadata.name does not match the computed hash", func(t *testing.T) {
		t.Parallel()
		project := newValidComposeProject("moby", "myproject")
		project.Name = "not-the-right-hash"
		_, err := v.ValidateCreate(context.Background(), project)
		assert.ErrorContains(t, err, "metadata.name must be")
	})

	t.Run("rejects an invalid action annotation", func(t *testing.T) {
		t.Parallel()
		project := newValidComposeProject("moby", "myproject")
		project.Annotations = map[string]string{
			v1alpha1.AnnotationAction: "bogus",
		}
		_, err := v.ValidateCreate(context.Background(), project)
		assert.ErrorContains(t, err, "invalid")
		assert.ErrorContains(t, err, v1alpha1.AnnotationAction)
	})

	t.Run("reports both a bad name and a bad annotation together", func(t *testing.T) {
		t.Parallel()
		project := newValidComposeProject("moby", "myproject")
		project.Name = "not-the-right-hash"
		project.Annotations = map[string]string{
			v1alpha1.AnnotationAction: "bogus",
		}
		_, err := v.ValidateCreate(context.Background(), project)
		assert.ErrorContains(t, err, "metadata.name must be")
		assert.ErrorContains(t, err, "invalid")
	})
}

func TestComposeProjectValidator_ValidateUpdate(t *testing.T) {
	t.Parallel()

	v := &composeProjectValidator{}

	// `metadata.name` immutability is enforced via CEL, so don't enforce here.
	t.Run("ignores metadata.name", func(t *testing.T) {
		t.Parallel()
		oldProject := newValidComposeProject("moby", "myproject")
		newProject := newValidComposeProject("moby", "myproject")
		newProject.Name = "not-the-right-hash"
		_, err := v.ValidateUpdate(context.Background(), oldProject, newProject)
		assert.NilError(t, err)
	})

	t.Run("accepts a valid action annotation", func(t *testing.T) {
		t.Parallel()
		oldProject := newValidComposeProject("moby", "myproject")
		newProject := newValidComposeProject("moby", "myproject")
		newProject.Annotations = map[string]string{
			v1alpha1.AnnotationAction: string(v1alpha1.ComposeProjectActionUp),
		}
		newProject.Spec.WorkingDir = t.TempDir()
		_, err := v.ValidateUpdate(context.Background(), oldProject, newProject)
		assert.NilError(t, err)
	})

	t.Run("rejects an invalid action annotation", func(t *testing.T) {
		t.Parallel()
		oldProject := newValidComposeProject("moby", "myproject")
		newProject := newValidComposeProject("moby", "myproject")
		newProject.Annotations = map[string]string{
			v1alpha1.AnnotationAction: "bogus",
		}
		_, err := v.ValidateUpdate(context.Background(), oldProject, newProject)
		assert.ErrorContains(t, err, "invalid")
		assert.ErrorContains(t, err, v1alpha1.AnnotationAction)
	})
}
