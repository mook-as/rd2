// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: SUSE LLC
// SPDX-FileCopyrightText: The Rancher Desktop Authors

package compose

import (
	"crypto/sha256"
	"fmt"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/validation"
)

// defaultReapDelay is the amount of time to wait before reaping an object after
// it has been marked for deletion.
const defaultReapDelay = 10 * time.Minute

// reapAnnotation is the annotation key used to allow overriding the reap delay.
// This is not supported; it is only used for testing.
const reapAnnotation = "containers.rancherdesktop.io/reap-after"

// mirrorFinalizer is added to mirror resources so user deletions
// are forwarded to the container engine before the resource is removed.
const mirrorFinalizer = "engine.rancherdesktop.io/mirror"

func getReapDelay(obj metav1.Object) time.Duration {
	if obj == nil {
		return defaultReapDelay
	}

	if val, ok := obj.GetAnnotations()[reapAnnotation]; ok {
		if dur, err := time.ParseDuration(val); err == nil {
			return dur
		}
	}

	return defaultReapDelay
}

// generateProjectName returns the expected metadata.name for a ComposeProject
// or ComposeUpRequest object with the given namespace and name (i.e.
// status.namespace/status.name for ComposeProject, or spec.namespace/spec.name
// for ComposeUpRequest).
//
// The candidate name is namespace, followed by a dot, followed by the name.
// If that candidate is a valid Kubernetes name, it is used
// directly as metadata.name; otherwise, metadata.name is "cmp-" followed by
// the lower-case SHA-256 hash of the candidate name.
func generateProjectName(namespace, name string) string {
	candidateName := namespace + "." + name
	if len(validation.IsDNS1123Subdomain(candidateName)) == 0 {
		return candidateName
	}
	return fmt.Sprintf("cmp-%x", sha256.Sum256([]byte(candidateName)))
}
