// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: The Rancher Desktop Authors

package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/rancher-sandbox/rancher-desktop-daemon/pkg/appxinstall"
)

func newInstallUpdateCommand() *cobra.Command {
	var forceAppShutdown bool
	cmd := &cobra.Command{
		Use:   "install-update <path-to-appx>",
		Short: "Install a downloaded APPX update package, replacing the running one",
		Long: `install-update registers the given .appx file as an in-place upgrade of the
currently-installed Rancher Desktop package, via the
Windows.Management.Deployment.PackageManager API.

This exists so Rancher Desktop's auto-updater can drive an APPX install
without spawning powershell.exe (whose Add-AppxPackage cmdlet this replaces),
since PowerShell is disabled or constrained by policy in some corporate
environments.`,
		Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			if err := appxinstall.Install(args[0], appxinstall.Options{ForceAppShutdown: forceAppShutdown}); err != nil {
				return fmt.Errorf("installing update: %w", err)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&forceAppShutdown, "force-app-shutdown", false,
		"Allow the install to terminate running processes of the package being replaced")
	return cmd
}
