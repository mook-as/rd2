// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: SUSE LLC
// SPDX-FileCopyrightText: The Rancher Desktop Authors

package controllers

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/lima-vm/lima/v2/pkg/limatype"
	"gotest.tools/v3/assert"
)

func TestWSL2RegistrationIsOrphaned(t *testing.T) {
	// newDir returns a fresh instance directory, optionally containing the
	// WSL2 root disk so the predicate sees a registration with its disk intact.
	newDir := func(t *testing.T, withDisk bool) string {
		t.Helper()
		dir := t.TempDir()
		if withDisk {
			assert.NilError(t, os.WriteFile(filepath.Join(dir, wsl2RootDisk), []byte("vhdx"), 0o644))
		}
		return dir
	}

	t.Run("registered WSL2 distro with a missing disk is orphaned", func(t *testing.T) {
		inst := &limatype.Instance{Dir: newDir(t, false), VMType: limatype.WSL2, Status: limatype.StatusStopped}
		assert.Assert(t, wsl2RegistrationIsOrphaned(inst))
	})

	t.Run("registered WSL2 distro with its disk present is healthy", func(t *testing.T) {
		inst := &limatype.Instance{Dir: newDir(t, true), VMType: limatype.WSL2, Status: limatype.StatusStopped}
		assert.Assert(t, !wsl2RegistrationIsOrphaned(inst))
	})

	t.Run("uninitialized WSL2 distro is not orphaned (fresh import pending)", func(t *testing.T) {
		inst := &limatype.Instance{Dir: newDir(t, false), VMType: limatype.WSL2, Status: limatype.StatusUninitialized}
		assert.Assert(t, !wsl2RegistrationIsOrphaned(inst))
	})

	t.Run("non-WSL2 instance with a missing disk is not orphaned", func(t *testing.T) {
		inst := &limatype.Instance{Dir: newDir(t, false), VMType: limatype.QEMU, Status: limatype.StatusStopped}
		assert.Assert(t, !wsl2RegistrationIsOrphaned(inst))
	})
}
