// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: SUSE LLC
// SPDX-FileCopyrightText: The Rancher Desktop Authors

package process

import (
	"os"
	"sync/atomic"
	"testing"
	"time"

	"gotest.tools/v3/assert"
)

// TestRegisterInterruptHandlerAndInterrupt confirms the named-event round trip:
// after a process registers a handler for a key, Interrupt(key, pid) fires it
// and IsOurProcess(key, pid) recognises it. This is the mechanism that delivers
// a graceful shutdown across consoles.
func TestRegisterInterruptHandlerAndInterrupt(t *testing.T) {
	const key = ServeInterruptKey
	var fired atomic.Bool
	release, err := RegisterInterruptHandler(key, func() { fired.Store(true) })
	assert.NilError(t, err)
	defer release()

	assert.Assert(t, IsOurProcess(key, os.Getpid()),
		"a process that registered key %q should be recognised as ours", key)
	assert.Assert(t, !IsOurProcess("invalid-key", os.Getpid()),
		"a key the process did not register must not match")

	assert.NilError(t, Interrupt(key, os.Getpid()))

	// onInterrupt runs on RegisterInterruptHandler's watcher goroutine.
	for range 100 {
		if fired.Load() {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	assert.Assert(t, fired.Load(), "interrupt handler did not fire within 10s")
}

// TestInterruptUnregisteredFails confirms Interrupt and IsOurProcess reject a PID
// with no registered event — a dead, recycled, or unrelated process — so callers
// fall back to a force kill instead of disturbing it.
func TestInterruptUnregisteredFails(t *testing.T) {
	unregistered := os.Getpid() + 1
	assert.Assert(t, Interrupt(ServeInterruptKey, unregistered) != nil,
		"interrupting a pid with no registered event should fail")
	assert.Assert(t, !IsOurProcess(ServeInterruptKey, unregistered),
		"a pid with no registered event is not ours")
}
