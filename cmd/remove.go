/*
Copyright © 2024 thin-edge.io <info@thin-edge.io>
*/
package cmd

import (
	"context"
	"log/slog"

	containerSDK "github.com/docker/docker/api/types/container"
	"github.com/docker/docker/errdefs"
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

		err = cli.Client.ContainerStop(ctx, containerName, containerSDK.StopOptions{})
		if err != nil {
			if errdefs.IsNotFound(err) {
				slog.Info("Container does not exist, so nothing to stop")
				return nil
			}
			slog.Warn("Could not stop the container.", "err", err)
			return err
		}
		err = cli.Client.ContainerRemove(ctx, containerName, containerSDK.RemoveOptions{
			RemoveVolumes: false,
			RemoveLinks:   false,
		})
		if err != nil {
			if errdefs.IsNotFound(err) {
				slog.Info("Container does not exist, so nothing to stop")
				return nil
			}
			slog.Warn("Could not remove the container.", "err", err)
		}

		return err
	},
}

func init() {
	containerCmd.AddCommand(removeCmd)

	removeCmd.Flags().StringVar(&removeCmdOptions.ModuleVersion, "module-version", "", "Software version to remove")
}
