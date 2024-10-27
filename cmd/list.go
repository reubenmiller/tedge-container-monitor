/*
Copyright © 2024 thin-edge.io <info@thin-edge.io>
*/
package cmd

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/spf13/cobra"
	"github.com/thin-edge/tedge-container-monitor/pkg/container"
)

// listCmd represents the list command
var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List containers",
	Long:  `List containers`,
	Args:  cobra.ExactArgs(0),
	RunE: func(cmd *cobra.Command, args []string) error {
		slog.Info("Executing", "cmd", cmd.CalledAs(), "args", args)
		ctx := context.Background()
		cli, err := container.NewContainerClient()
		if err != nil {
			return err
		}
		containers, err := cli.List(ctx, config.GetFilterOptions())
		if err != nil {
			return err
		}
		stdout := cmd.OutOrStdout()
		for _, item := range containers {
			if item.ServiceType == container.ContainerType {
				version := item.Container.Image[strings.LastIndex(item.Container.Image, "/")+1:]
				fmt.Fprintf(stdout, "%s\t%s\n", item.Name, version)
			}
		}
		return nil
	},
}

func init() {
	containerCmd.AddCommand(listCmd)
}
