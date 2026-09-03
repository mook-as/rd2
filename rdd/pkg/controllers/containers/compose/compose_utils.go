// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: SUSE LLC
// SPDX-FileCopyrightText: The Rancher Desktop Authors

package compose

import (
	"crypto/sha256"
	"fmt"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// defaultReapDelay is the amount of time to wait before reaping an object after
// it has been marked for deletion.
const defaultReapDelay = 10 * time.Minute

// reapAnnotation is the annotation key used to allow overriding the reap delay.
// This is not supported; it is only used for testing.
const reapAnnotation = "containers.rancherdesktop.io/reap-after"

// generateProjectName returns the expected metadata.name for a Compose or
// ComposeUpRequest object with the given namespace and name (i.e.
// status.namespace/status.name for Compose, or spec.namespace/spec.name for
// ComposeUpRequest).
func generateProjectName(namespace, name string) string {
	constructedName := namespace + "/" + strings.ToLower(name)
	return fmt.Sprintf("%x", sha256.Sum256([]byte(constructedName)))
}

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
