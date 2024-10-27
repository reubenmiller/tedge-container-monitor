/*
Copyright © 2024 thin-edge.io <info@thin-edge.io>
*/
package cmd

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"github.com/thin-edge/tedge-container-monitor/pkg/app"
	"github.com/thin-edge/tedge-container-monitor/pkg/container"
	"github.com/thin-edge/tedge-container-monitor/pkg/tedge"
)

type Config struct {
	TopicRoot     string
	TopicID       string
	ServiceName   string
	DeviceID      string
	Names         []string
	Labels        []string
	RunOnce       bool
	Interval      time.Duration
	FilterOptions container.FilterOptions
}

var config *Config

// Protect against misconfiguration by setting a minimum allowed value
var MinimumPollingInterval = 60 * time.Second

// runCmd represents the run command
var runCmd = &cobra.Command{
	Use:   "run",
	Short: "Run the container monitor",
	Long: `Start the container monitor which will periodically publish container information
to the thin-edge.io interface.
`,
	RunE: func(cmd *cobra.Command, args []string) error {
		device := tedge.NewTarget(config.TopicRoot, config.TopicID)
		device.CloudIdentity = config.DeviceID
		application, err := app.NewApp(*device, app.Config{
			ServiceName:        config.ServiceName,
			EnableMetrics:      false,
			DeleteFromCloud:    true,
			EnableEngineEvents: true,
		})
		if err != nil {
			return err
		}

		// FIXME: Wait until the entity store has been filled
		time.Sleep(200 * time.Millisecond)

		if config.RunOnce {
			// Cleanly stop the application in run-once mode
			// so that the service still appears to be "up" as the Last Will and Testament
			// message should not be sent (as the exit is expected)
			// This logic is similar to SystemD's RemainAfterExit=yes setting
			defer application.Stop(true)
			return application.Update(config.FilterOptions)
		}

		// if err := application.Subscribe(); err != nil {
		// 	slog.Error("Failed to subscribe to commands.", "err", err)
		// 	return err
		// }

		if err := application.Update(config.FilterOptions); err != nil {
			slog.Warn("Failed to update container state.", "err", err)
		}

		stop := make(chan os.Signal, 1)
		signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

		// Start background monitor
		ctx, cancel := context.WithCancel(context.Background())
		go application.Monitor(ctx, container.FilterOptions{})
		<-stop
		cancel()
		application.Stop(false)
		slog.Info("Shutting down...")
		return nil
	},
}

func init() {
	config = &Config{
		FilterOptions: container.FilterOptions{},
	}
	rootCmd.AddCommand(runCmd)
	runCmd.Flags().StringVar(&config.DeviceID, "device-id", "", "thin-edge.io device id")
	runCmd.Flags().StringVar(&config.ServiceName, "service-name", "tedge-container-monitor", "Service name")
	runCmd.Flags().StringSliceVar(&config.FilterOptions.Names, "name", []string{}, "Only include given container names")
	runCmd.Flags().StringSliceVar(&config.FilterOptions.Labels, "label", []string{}, "Only include containers with the given labels")
	runCmd.Flags().StringSliceVar(&config.FilterOptions.IDs, "id", []string{}, "Only include containers with the given ids")
	runCmd.Flags().StringSliceVar(&config.FilterOptions.Types, "type", []string{container.ContainerType, container.ContainerGroupType}, "Filter by container type")
	runCmd.Flags().StringVar(&config.TopicRoot, "mqtt-topic-root", "te", "MQTT root prefix")
	runCmd.Flags().StringVar(&config.TopicID, "mqtt-device-topic-id", "device/main//", "The device MQTT topic identifier")
	runCmd.Flags().BoolVar(&config.RunOnce, "once", false, "Only run the monitor once")
	runCmd.Flags().DurationVar(&config.Interval, "interval", 60*time.Second, "Polling interval")
}
