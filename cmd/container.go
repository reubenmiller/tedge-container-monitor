/*
Copyright © 2024 thin-edge.io <info@thin-edge.io>
*/
package cmd

import (
	"github.com/spf13/cobra"
)

var containerConfig ContainerConfig

type ContainerConfig struct {
	PruneImages bool
}

// containerCmd represents the container command
var containerCmd = &cobra.Command{
	Use:   "container",
	Short: "container software management plugin",
	Long:  `Install/Remove containers via the thin-edge.io software management plugin API`,
}

func init() {
	rootCmd.AddCommand(containerCmd)

	containerConfig = ContainerConfig{
		PruneImages: true,
	}
}
