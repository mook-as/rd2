// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: SUSE LLC
// SPDX-FileCopyrightText: The Rancher Desktop Authors

package composeproject

import (
	"crypto/sha256"
	"fmt"
	"strings"
)

// generateProjectName returns the expected metadata.name for a ComposeProject
// with the given spec.namespace and spec.name.
func generateProjectName(namespace, name string) string {
	constructedName := namespace + "/" + strings.ToLower(name)
	return fmt.Sprintf("%x", sha256.Sum256([]byte(constructedName)))
}
