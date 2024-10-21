/*
Copyright © 2024 thin-edge.io <info@thin-edge.io>
*/
package cmd

import (
	"github.com/spf13/cobra"
	"github.com/thin-edge/tedge-container-monitor/pkg/app"
	"github.com/thin-edge/tedge-container-monitor/pkg/tedge"
)

type Config struct {
	TopicRoot string
	TopicID   string
	Name      string
}

var config *Config

// runCmd represents the run command
var runCmd = &cobra.Command{
	Use:   "run",
	Short: "Run the container monitor",
	Long: `Start the container monitor which will periodically publish container information
to the thin-edge.io interface.
`,
	RunE: func(cmd *cobra.Command, args []string) error {
		device := tedge.NewTarget(config.TopicRoot, config.TopicID)
		application, err := app.NewApp(*device, config.Name)
		if err != nil {
			return err
		}
		return application.Update()
	},
}

func init() {
	config = &Config{}
	rootCmd.AddCommand(runCmd)
	runCmd.Flags().StringVar(&config.Name, "name", "tedge-container-monitor", "Service name")
	runCmd.Flags().StringVar(&config.TopicRoot, "mqtt-topic-root", "te", "MQTT root prefix")
	runCmd.Flags().StringVar(&config.TopicID, "mqtt-device-topic-id", "device/main//", "The device MQTT topic identifier")
}
