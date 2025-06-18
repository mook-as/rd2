// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: SUSE LLC
// SPDX-FileCopyrightText: The Rancher Desktop Authors

package controllers

import (
	"fmt"
	"net"
	"strconv"
)

// GetAvailablePort tries to use the desired port, but picks a random available port if it's not available.
// Returns the port number that was successfully bound.
func GetAvailablePort(desiredPort int) (int, error) {
	// If desired port is 0, let the system pick a random available port
	if desiredPort == 0 {
		listener, err := net.Listen("tcp", ":0")
		if err != nil {
			return 0, fmt.Errorf("failed to find available port: %w", err)
		}
		defer listener.Close()

		// Extract the port from the listener's address
		addr := listener.Addr().(*net.TCPAddr)
		return addr.Port, nil
	}

	// First, try the desired port
	if isPortAvailable(desiredPort) {
		return desiredPort, nil
	}

	// If desired port is not available, let the system pick a random available port
	listener, err := net.Listen("tcp", ":0")
	if err != nil {
		return 0, fmt.Errorf("failed to find available port: %w", err)
	}
	defer listener.Close()

	// Extract the port from the listener's address
	addr := listener.Addr().(*net.TCPAddr)
	return addr.Port, nil
}

// isPortAvailable checks if a port is available by trying to bind to it.
func isPortAvailable(port int) bool {
	address := ":" + strconv.Itoa(port)
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return false
	}
	defer listener.Close()
	return true
}
