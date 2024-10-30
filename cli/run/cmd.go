/*
Copyright © 2024 thin-edge.io <info@thin-edge.io>
*/
package run

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/thin-edge/tedge-container-monitor/pkg/app"
	"github.com/thin-edge/tedge-container-monitor/pkg/cli"
	"github.com/thin-edge/tedge-container-monitor/pkg/container"
)

var (
	DefaultServiceName = "tedge-container-monitor"
	DefaultTopicRoot   = "te"
	DefaultTopicPrefix = "device/main//"
)

type RunCommand struct {
	*cobra.Command

	RunOnce bool
}

func NewRunCommand(cliContext cli.Cli) *cobra.Command {
	// runCmd represents the run command
	command := &RunCommand{}
	cmd := &cobra.Command{
		Use:   "run",
		Short: "Run the container monitor",
		Long: `Start the container monitor which will periodically publish container information
	to the thin-edge.io interface.
	`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cliContext.PrintConfig()

			device := cliContext.GetDeviceTarget()
			application, err := app.NewApp(device, app.Config{
				ServiceName:        cliContext.GetServiceName(),
				EnableMetrics:      cliContext.MetricsEnabled(),
				DeleteFromCloud:    cliContext.DeleteFromCloud(),
				EnableEngineEvents: cliContext.EngineEventsEnabled(),

				MQTTHost:       cliContext.GetMQTTHost(),
				MQTTPort:       cliContext.GetMQTTPort(),
				CumulocityHost: cliContext.GetCumulocityHost(),
				CumulocityPort: cliContext.GetCumulocityPort(),

				KeyFile:  cliContext.GetKeyFile(),
				CertFile: cliContext.GetCertificateFile(),
				CAFile:   cliContext.GetCAFile(),
			})
			if err != nil {
				return err
			}

			// FIXME: Wait until the entity store has been filled
			time.Sleep(200 * time.Millisecond)

			if command.RunOnce {
				// Cleanly stop the application in run-once mode
				// so that the service still appears to be "up" as the Last Will and Testament
				// message should not be sent (as the exit is expected)
				// This logic is similar to SystemD's RemainAfterExit=yes setting
				defer application.Stop(true)
				return application.Update(cliContext.GetFilterOptions())
			}

			if err := application.Update(cliContext.GetFilterOptions()); err != nil {
				slog.Warn("Failed to update container state.", "err", err)
			}

			stop := make(chan os.Signal, 1)
			signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

			// Start background monitor
			ctx, cancel := context.WithCancel(context.Background())
			go application.Monitor(ctx, container.FilterOptions{})

			if cliContext.MetricsEnabled() {
				go backgroundMetric(ctx, cliContext, application, cliContext.GetMetricsInterval())
			}

			<-stop
			cancel()
			application.Stop(false)
			slog.Info("Shutting down...")
			return nil
		},
	}

	cmd.Flags().String("service-name", DefaultServiceName, "Service name")
	cmd.Flags().StringSlice("name", []string{}, "Only include given container names")
	cmd.Flags().StringSlice("label", []string{}, "Only include containers with the given labels")
	cmd.Flags().StringSlice("id", []string{}, "Only include containers with the given ids")
	cmd.Flags().StringSlice("type", []string{container.ContainerType, container.ContainerGroupType}, "Filter by container type")
	cmd.Flags().String("mqtt-topic-root", DefaultTopicRoot, "MQTT root prefix")
	cmd.Flags().String("mqtt-device-topic-id", DefaultTopicPrefix, "The device MQTT topic identifier")
	cmd.Flags().BoolVar(&command.RunOnce, "once", false, "Only run the monitor once")
	cmd.Flags().String("device-id", "", "thin-edge.io device id")
	cmd.Flags().Duration("interval", 300*time.Second, "Metrics update interval")

	//
	// viper bindings

	// Service
	viper.SetDefault("monitor.service_name", DefaultServiceName)
	viper.BindPFlag("monitor.service_name", cmd.Flags().Lookup("service-name"))

	// MQTT topics
	viper.SetDefault("monitor.mqtt.topic_root", DefaultTopicRoot)
	viper.BindPFlag("monitor.mqtt.topic_root", cmd.Flags().Lookup("mqtt-topic-root"))
	viper.SetDefault("monitor.mqtt.device_topic_id", DefaultTopicPrefix)
	viper.BindPFlag("monitor.mqtt.device_topic_id", cmd.Flags().Lookup("mqtt-device-topic-id"))
	viper.BindPFlag("monitor.mqtt.device_id", cmd.Flags().Lookup("device-id"))

	// Include filters
	viper.BindPFlag("monitor.filter.include.names", cmd.Flags().Lookup("name"))
	viper.BindPFlag("monitor.filter.include.labels", cmd.Flags().Lookup("label"))
	viper.BindPFlag("monitor.filter.include.ids", cmd.Flags().Lookup("id"))
	viper.BindPFlag("monitor.filter.include.types", cmd.Flags().Lookup("type"))

	// Exclude filters
	viper.SetDefault("monitor.filter.exclude.names", "")
	viper.SetDefault("monitor.filter.exclude.labels", "")

	// Metrics
	viper.BindPFlag("monitor.metrics.interval", cmd.Flags().Lookup("interval"))
	viper.SetDefault("monitor.metrics.interval", "300s")
	viper.SetDefault("monitor.metrics.enabled", true)

	// Feature flags
	viper.SetDefault("monitor.events.enabled", true)
	viper.SetDefault("monitor.delete_from_cloud.enabled", true)

	// thin-edge.io services
	viper.SetDefault("monitor.mqtt.client.host", "127.0.0.1")
	viper.SetDefault("monitor.mqtt.client.port", "1883")
	viper.SetDefault("monitor.c8y.proxy.client.host", "127.0.0.1")
	viper.SetDefault("monitor.c8y.proxy.client.port", "8001")

	// TLS
	viper.SetDefault("monitor.client.key", "")
	viper.SetDefault("monitor.client.cert_file", "")
	viper.SetDefault("monitor.client.ca_file", "")

	command.Command = cmd
	return cmd
}

func backgroundMetric(ctx context.Context, cliContext cli.Cli, application *app.App, interval time.Duration) error {
	timerCh := time.NewTicker(interval)
	for {
		select {
		case <-ctx.Done():
			slog.Info("Stopping metrics task")
			return ctx.Err()

		case <-timerCh.C:
			slog.Info("Refreshing metrics")
			application.UpdateMetrics(cliContext.GetFilterOptions())
		}
	}
}
