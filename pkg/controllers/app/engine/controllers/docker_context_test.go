// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: SUSE LLC
// SPDX-FileCopyrightText: The Rancher Desktop Authors

package controllers

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"gotest.tools/v3/assert"
)

// newDockerTestDir creates a temp ~/.docker layout and points HOME at its parent.
// Returns the path to the config.json that will be used.
func newDockerTestDir(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	dockerDir := filepath.Join(root, ".docker")
	assert.NilError(t, os.MkdirAll(dockerDir, 0o700))
	t.Setenv("HOME", root)
	// os.UserHomeDir() reads USERPROFILE on Windows, not HOME.
	t.Setenv("USERPROFILE", root)
	// DOCKER_CONFIG would override HOME; clear it so HOME wins.
	t.Setenv("DOCKER_CONFIG", "")
	return filepath.Join(dockerDir, "config.json")
}

func Test_createReplaceDockerContext(t *testing.T) {
	newDockerTestDir(t)

	assert.NilError(t, createReplaceDockerContext("rancher-desktop-2", "/tmp/docker.sock"))

	host, err := getDockerContextHost("rancher-desktop-2")
	assert.NilError(t, err)
	assert.Equal(t, host, "unix:///tmp/docker.sock")

	// The Description is surfaced by `docker context ls`; pin the value.
	s, err := newContextStore()
	assert.NilError(t, err)
	md, err := s.GetMetadata("rancher-desktop-2")
	assert.NilError(t, err)
	meta, ok := md.Metadata.(map[string]any)
	assert.Assert(t, ok, "Metadata must decode as map[string]any")
	assert.Equal(t, meta["Description"], "Rancher Desktop rancher-desktop-2")

	// Replacing with a new socket updates the host.
	assert.NilError(t, createReplaceDockerContext("rancher-desktop-2", "/run/docker.sock"))
	host, err = getDockerContextHost("rancher-desktop-2")
	assert.NilError(t, err)
	assert.Equal(t, host, "unix:///run/docker.sock")
}

func Test_dockerConfigDir_DOCKER_CONFIG(t *testing.T) {
	// HOME points somewhere we do NOT want the context to land.
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	// DOCKER_CONFIG takes precedence over HOME.
	override := t.TempDir()
	t.Setenv("DOCKER_CONFIG", override)

	dir, err := dockerConfigDir()
	assert.NilError(t, err)
	assert.Equal(t, dir, override)

	// Context files land under DOCKER_CONFIG, not HOME/.docker.
	assert.NilError(t, createReplaceDockerContext("rancher-desktop-2", "/tmp/docker.sock"))
	_, err = os.Stat(filepath.Join(override, "contexts"))
	assert.NilError(t, err, "contexts/ must exist under DOCKER_CONFIG")
	_, err = os.Stat(filepath.Join(home, ".docker", "contexts"))
	assert.Assert(t, os.IsNotExist(err), "no contexts/ must appear under HOME/.docker")
}

func Test_getCurrentDockerContext_malformedAuth(t *testing.T) {
	configFile := newDockerTestDir(t)

	// config.Load decodes every auths[*].auth; invalid base64 fails the call.
	seed := []byte(`{"auths":{"example.com":{"auth":"not-base64"}}}` + "\n")
	assert.NilError(t, os.WriteFile(configFile, seed, 0o600))

	_, err := getCurrentDockerContext()
	assert.Assert(t, err != nil, "malformed auths entry must surface as an error")
}

func Test_getDockerContextHost_missing(t *testing.T) {
	newDockerTestDir(t)
	host, err := getDockerContextHost("does-not-exist")
	assert.NilError(t, err)
	assert.Equal(t, host, "")
}

func Test_deleteDockerContext(t *testing.T) {
	newDockerTestDir(t)

	assert.NilError(t, createReplaceDockerContext("rancher-desktop-2", "/tmp/docker.sock"))
	host, err := getDockerContextHost("rancher-desktop-2")
	assert.NilError(t, err)
	assert.Equal(t, host, "unix:///tmp/docker.sock")

	assert.NilError(t, deleteDockerContext("rancher-desktop-2"))
	host, err = getDockerContextHost("rancher-desktop-2")
	assert.NilError(t, err)
	assert.Equal(t, host, "")

	// Second delete is a no-op.
	assert.NilError(t, deleteDockerContext("rancher-desktop-2"))
}

func Test_currentDockerContext(t *testing.T) {
	configFile := newDockerTestDir(t)

	t.Run("returns empty when file absent", func(t *testing.T) {
		name, err := getCurrentDockerContext()
		assert.NilError(t, err)
		assert.Equal(t, name, "")
	})

	t.Run("set then get", func(t *testing.T) {
		assert.NilError(t, setCurrentDockerContext("rancher-desktop-2"))
		name, err := getCurrentDockerContext()
		assert.NilError(t, err)
		assert.Equal(t, name, "rancher-desktop-2")
	})

	t.Run("clear only when context matches", func(t *testing.T) {
		assert.NilError(t, setCurrentDockerContext("rancher-desktop-2"))
		// Different name — should not clear.
		assert.NilError(t, clearCurrentDockerContext("rancher-desktop-3"))
		name, err := getCurrentDockerContext()
		assert.NilError(t, err)
		assert.Equal(t, name, "rancher-desktop-2")

		// Matching name — should clear.
		assert.NilError(t, clearCurrentDockerContext("rancher-desktop-2"))
		name, err = getCurrentDockerContext()
		assert.NilError(t, err)
		assert.Equal(t, name, "")
	})

	t.Run("set preserves known keys in config.json", func(t *testing.T) {
		// "auths" is a known field in ConfigFile, so it survives a save.
		seed := []byte(`{"auths":{"example.com":{}}}` + "\n")
		assert.NilError(t, os.WriteFile(configFile, seed, 0o600))

		assert.NilError(t, setCurrentDockerContext("rancher-desktop-2"))

		data, err := os.ReadFile(configFile)
		assert.NilError(t, err)
		var cfg map[string]any
		assert.NilError(t, json.Unmarshal(data, &cfg))
		assert.Equal(t, cfg["currentContext"], "rancher-desktop-2")
		_, hasAuths := cfg["auths"]
		assert.Assert(t, hasAuths, "auths key must be preserved")
	})

	t.Run("set drops unknown top-level keys", func(t *testing.T) {
		// docker/cli's ConfigFile is a closed struct; unknown top-level keys
		// are lost on save. `docker context use` has the same behavior.
		seed := []byte(`{"rancherDesktopCustom":"keep-me"}` + "\n")
		assert.NilError(t, os.WriteFile(configFile, seed, 0o600))

		assert.NilError(t, setCurrentDockerContext("rancher-desktop-2"))

		data, err := os.ReadFile(configFile)
		assert.NilError(t, err)
		var cfg map[string]any
		assert.NilError(t, json.Unmarshal(data, &cfg))
		assert.Equal(t, cfg["currentContext"], "rancher-desktop-2")
		_, hasCustom := cfg["rancherDesktopCustom"]
		assert.Assert(t, !hasCustom, "unknown top-level keys are dropped on save")
	})

	t.Run("clear preserves known keys in config.json", func(t *testing.T) {
		seed := []byte(`{"currentContext":"rancher-desktop-2","auths":{"example.com":{}}}` + "\n")
		assert.NilError(t, os.WriteFile(configFile, seed, 0o600))

		assert.NilError(t, clearCurrentDockerContext("rancher-desktop-2"))

		name, err := getCurrentDockerContext()
		assert.NilError(t, err)
		assert.Equal(t, name, "")

		data, err := os.ReadFile(configFile)
		assert.NilError(t, err)
		var cfg map[string]any
		assert.NilError(t, json.Unmarshal(data, &cfg))
		_, hasAuths := cfg["auths"]
		assert.Assert(t, hasAuths, "auths key must be preserved across clear")
	})
}
