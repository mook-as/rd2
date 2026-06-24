//go:build unix

// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: SUSE LLC
// SPDX-FileCopyrightText: The Rancher Desktop Authors

package process

import (
	"os"
	"testing"

	"gotest.tools/v3/assert"
)

// TestIsOurProcess confirms the Unix identity check reports liveness only — the
// key is ignored — and rejects a non-positive PID.
func TestIsOurProcess(t *testing.T) {
	assert.Assert(t, IsOurProcess(ServeInterruptKey, os.Getpid()),
		"the live test process is ours")
	assert.Assert(t, !IsOurProcess(ServeInterruptKey, 0x7FFFFFF0),
		"an unassigned pid is not ours")
	assert.Assert(t, !IsOurProcess(ServeInterruptKey, 0),
		"a non-positive pid is never ours")
}
