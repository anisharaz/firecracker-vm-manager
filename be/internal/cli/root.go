// Package cli wires the cobra command tree for the manager binary.
package cli

import (
	"github.com/spf13/cobra"
)

// NewRootCmd returns the root command. Subcommands attach themselves below.
func NewRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "firecracker-manager",
		Short:         "Manage Firecracker microVMs over a local HTTP API",
		SilenceUsage:  true,
		SilenceErrors: false,
	}

	root.PersistentFlags().StringP("config", "c", "", "path to YAML config file")

	root.AddCommand(newStartCmd())
	root.AddCommand(newValidateCmd())

	return root
}
