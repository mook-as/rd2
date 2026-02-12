// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: SUSE LLC
// SPDX-FileCopyrightText: The Rancher Desktop Authors

package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/rancher-sandbox/rancher-desktop-daemon/pkg/instance"
)

func instancePaths() map[string]string {
	return map[string]string{
		"dir":        instance.Dir(),
		"log_dir":    instance.LogDir(),
		"short_dir":  instance.ShortDir(),
		"lima_home":  instance.LimaHome(),
		"tls_dir":    instance.TLSDir(),
		"kubeconfig": instance.KubeConfig(),
		"pid_file":   instance.PIDFile(),
		"args_file":  instance.ArgsFile(),
	}
}

func newServicePathsCommand() *cobra.Command {
	command := &cobra.Command{
		Use:   "paths [key]",
		Short: "Print instance paths",
		Args:  cobra.MaximumNArgs(1),
		RunE:  servicePathsAction,
	}
	command.Flags().Bool("json", false, "Output as JSON")
	command.Flags().Bool("shell", false, "Output as shell export statements")
	return command
}

func servicePathsAction(cmd *cobra.Command, args []string) error {
	asJSON, _ := cmd.Flags().GetBool("json")
	asShell, _ := cmd.Flags().GetBool("shell")
	if asJSON && asShell {
		return errors.New("--json and --shell are mutually exclusive")
	}

	paths := instancePaths()
	keys := slices.Sorted(maps.Keys(paths))

	// Filter to a single key if specified.
	if len(args) == 1 {
		key := args[0]
		if _, ok := paths[key]; !ok {
			return fmt.Errorf("unknown key %q; valid keys: %s", key, strings.Join(keys, ", "))
		}
		if !asJSON && !asShell {
			_, err := fmt.Fprintln(cmd.OutOrStdout(), paths[key])
			return err
		}
		keys = []string{key}
	}

	w := cmd.OutOrStdout()
	switch {
	case asJSON:
		m := make(map[string]string, len(keys))
		for _, key := range keys {
			m[key] = paths[key]
		}
		return json.NewEncoder(w).Encode(m)
	case asShell:
		for _, key := range keys {
			if _, err := fmt.Fprintf(w, "export RDD_%s=%q\n", strings.ToUpper(key), paths[key]); err != nil {
				return err
			}
		}
		return nil
	default:
		maxKey := 0
		for _, key := range keys {
			if len(key) > maxKey {
				maxKey = len(key)
			}
		}
		for _, key := range keys {
			if _, err := fmt.Fprintf(w, "%-*s  %s\n", maxKey, key, paths[key]); err != nil {
				return err
			}
		}
		return nil
	}
}
