/*
Copyright © 2024 thin-edge.io <info@thin-edge.io>
*/
package cmd

import (
	"context"
	"log/slog"

	"github.com/spf13/cobra"
	"github.com/thin-edge/tedge-container-monitor/pkg/container"
)

var removeCmdOptions removeOptions

type removeOptions struct {
	ModuleVersion string
}

// removeCmd represents the remove command
var removeCmd = &cobra.Command{
	Use:   "remove",
	Short: "Remove a container",
	Long:  `Remove a container`,
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		slog.Info("Executing", "cmd", cmd.CalledAs(), "args", args)
		ctx := context.Background()
		containerName := args[0]

		cli, err := container.NewContainerClient()
		if err != nil {
			return err
		}

		return cli.StopRemoveContainer(ctx, containerName)
	},
}

func init() {
	containerCmd.AddCommand(removeCmd)

	removeCmd.Flags().StringVar(&removeCmdOptions.ModuleVersion, "module-version", "", "Software version to remove")
}
