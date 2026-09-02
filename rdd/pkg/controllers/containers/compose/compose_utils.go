// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: SUSE LLC
// SPDX-FileCopyrightText: The Rancher Desktop Authors

package compose

import (
	"crypto/sha256"
	"fmt"
	"strings"
)

// generateProjectName returns the expected metadata.name for a Compose or
// ComposeUpRequest object with the given namespace and name (i.e.
// status.namespace/status.name for Compose, or spec.namespace/spec.name for
// ComposeUpRequest).
func generateProjectName(namespace, name string) string {
	constructedName := namespace + "/" + strings.ToLower(name)
	return fmt.Sprintf("%x", sha256.Sum256([]byte(constructedName)))
}
