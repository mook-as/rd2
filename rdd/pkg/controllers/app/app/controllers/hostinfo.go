// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: SUSE LLC
// SPDX-FileCopyrightText: The Rancher Desktop Authors

package controllers

import (
	goruntime "runtime"

	"github.com/pbnjay/memory"
)

// HostInfo holds the detected host hardware limits used to validate VM resource requests.
type HostInfo struct {
	// CPUs is the number of logical CPUs on the host.
	CPUs int
	// Memory is the total host memory in bytes.
	Memory int64
}

// DetectHostInfo reads the host CPU count and total memory.
func DetectHostInfo() HostInfo {
	return HostInfo{
		CPUs:   goruntime.NumCPU(),
		Memory: int64(memory.TotalMemory()),
	}
}
