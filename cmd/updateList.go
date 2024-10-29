/*
Copyright © 2024 thin-edge.io <info@thin-edge.io>
*/
package cmd

import (
	"log/slog"
	"os"

	"github.com/spf13/cobra"
)

// updateListCmd represents the updateList command
var updateListCmd = &cobra.Command{
	Use:   "update-list",
	Short: "Install/remove a list of containers",
	Long:  `Not implemented`,
	Run: func(cmd *cobra.Command, args []string) {
		slog.Info("update-list is not supported")
		os.Exit(1)
	},
}

func init() {
	containerCmd.AddCommand(updateListCmd)
}
