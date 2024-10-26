/*
Copyright © 2024 thin-edge.io <info@thin-edge.io>
*/
package cmd

import (
	"log/slog"

	"github.com/spf13/cobra"
)

// prepareCmd represents the prepare command
var prepareCmd = &cobra.Command{
	Use:   "prepare",
	Short: "Prepare for install/removal",
	Long:  `Prepare for install/removal`,
	Run: func(cmd *cobra.Command, args []string) {
		slog.Info("Executing", "cmd", cmd.CalledAs(), "args", args)
	},
}

func init() {
	containerCmd.AddCommand(prepareCmd)
}
