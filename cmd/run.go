/*
Copyright © 2024 thin-edge.io <info@thin-edge.io>
*/
package cmd

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/thin-edge/tedge-container-monitor/pkg/app"
	"github.com/thin-edge/tedge-container-monitor/pkg/container"
	"github.com/thin-edge/tedge-container-monitor/pkg/tedge"
)

type Config struct {
	RunOnce bool
}

func (c *Config) PrintConfig() {
	keys := viper.AllKeys()
	sort.Strings(keys)
	for _, key := range keys {
		slog.Info("setting", "item", fmt.Sprintf("%s=%v", key, viper.Get(key)))
	}
}

func (c *Config) GetServiceName() string {
	return viper.GetString("monitor.service_name")
}

func (c *Config) GetKeyFile() string {
	return viper.GetString("monitor.client.key")
}

func (c *Config) GetCertificateFile() string {
	return viper.GetString("monitor.client.cert_file")
}

func (c *Config) GetCAFile() string {
	return viper.GetString("monitor.client.ca_file")
}

func (c *Config) GetTopicRoot() string {
	return viper.GetString("monitor.mqtt.topic_root")
}

func (c *Config) GetTopicID() string {
	return viper.GetString("monitor.mqtt.device_topic_id")
}

func (c *Config) GetDeviceID() string {
	return viper.GetString("monitor.mqtt.device_id")
}

func (c *Config) MetricsEnabled() bool {
	return viper.GetBool("monitor.metrics.enabled")
}

func (c *Config) EngineEventsEnabled() bool {
	return viper.GetBool("monitor.events.enabled")
}

func (c *Config) DeleteFromCloud() bool {
	return viper.GetBool("monitor.delete_from_cloud.enabled")
}

func (c *Config) GetMQTTHost() string {
	return viper.GetString("monitor.mqtt.client.host")
}

func (c *Config) GetMetricsInterval() time.Duration {
	interval := viper.GetDuration("monitor.metrics.interval")
	if interval < 60*time.Second {
		slog.Warn("monitor.metrics.interval is lower than allowed limit.", "old", interval, "new", 60*time.Second)
		interval = 60 * time.Second
	}
	return interval
}

func (c *Config) GetMQTTPort() uint16 {
	v := viper.GetUint16("monitor.mqtt.client.port")
	if v == 0 {
		return 1883
	}
	return v
}

func (c *Config) GetCumulocityHost() string {
	return viper.GetString("monitor.c8y.proxy.client.host")
}

func (c *Config) GetCumulocityPort() uint16 {
	v := viper.GetUint16("monitor.c8y.proxy.client..port")
	if v == 0 {
		return 8001
	}
	return v
}

func (c *Config) GetDeviceTarget() tedge.Target {
	return tedge.Target{
		RootPrefix:    c.GetTopicRoot(),
		TopicID:       c.GetTopicID(),
		CloudIdentity: c.GetDeviceID(),
	}
}

func getExpandedStringSlice(key string) []string {
	v := viper.GetStringSlice(key)
	out := make([]string, 0, len(v))
	for _, item := range v {
		out = append(out, strings.Split(item, ",")...)
	}
	return out
}

func (c *Config) GetFilterOptions() container.FilterOptions {
	options := container.FilterOptions{
		Names:            getExpandedStringSlice("monitor.filter.include.names"),
		IDs:              getExpandedStringSlice("monitor.filter.include.ids"),
		Labels:           getExpandedStringSlice("monitor.filter.include.labels"),
		Types:            getExpandedStringSlice("monitor.filter.include.types"),
		ExcludeNames:     getExpandedStringSlice("monitor.filter.exclude.names"),
		ExcludeWithLabel: getExpandedStringSlice("monitor.filter.exclude.labels"),
	}
	return options
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
		config.PrintConfig()

		device := config.GetDeviceTarget()
		application, err := app.NewApp(device, app.Config{
			ServiceName:        config.GetServiceName(),
			EnableMetrics:      config.MetricsEnabled(),
			DeleteFromCloud:    config.DeleteFromCloud(),
			EnableEngineEvents: config.EngineEventsEnabled(),

			MQTTHost:       config.GetMQTTHost(),
			MQTTPort:       config.GetMQTTPort(),
			CumulocityHost: config.GetCumulocityHost(),
			CumulocityPort: config.GetCumulocityPort(),

			KeyFile:  config.GetKeyFile(),
			CertFile: config.GetCertificateFile(),
			CAFile:   config.GetCAFile(),
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
			return application.Update(config.GetFilterOptions())
		}

		// if err := application.Subscribe(); err != nil {
		// 	slog.Error("Failed to subscribe to commands.", "err", err)
		// 	return err
		// }

		if err := application.Update(config.GetFilterOptions()); err != nil {
			slog.Warn("Failed to update container state.", "err", err)
		}

		stop := make(chan os.Signal, 1)
		signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

		// Start background monitor
		ctx, cancel := context.WithCancel(context.Background())
		go application.Monitor(ctx, container.FilterOptions{})

		if config.MetricsEnabled() {
			go backgroundMetric(ctx, application, config.GetMetricsInterval())
		}

		<-stop
		cancel()
		application.Stop(false)
		slog.Info("Shutting down...")
		return nil
	},
}

func backgroundMetric(ctx context.Context, application *app.App, interval time.Duration) error {
	timerCh := time.NewTicker(interval)
	for {
		select {
		case <-ctx.Done():
			slog.Info("Stopping metrics task")
			return ctx.Err()

		case <-timerCh.C:
			slog.Info("Refreshing metrics")
			application.UpdateMetrics(config.GetFilterOptions())
		}
	}
}

func init() {
	config = &Config{}

	DefaultServiceName := "tedge-container-monitor"
	DefaultTopicRoot := "te"
	DefaultTopicPrefix := "device/main//"

	rootCmd.AddCommand(runCmd)
	runCmd.Flags().String("service-name", DefaultServiceName, "Service name")
	runCmd.Flags().StringSlice("name", []string{}, "Only include given container names")
	runCmd.Flags().StringSlice("label", []string{}, "Only include containers with the given labels")
	runCmd.Flags().StringSlice("id", []string{}, "Only include containers with the given ids")
	runCmd.Flags().StringSlice("type", []string{container.ContainerType, container.ContainerGroupType}, "Filter by container type")
	runCmd.Flags().String("mqtt-topic-root", DefaultTopicRoot, "MQTT root prefix")
	runCmd.Flags().String("mqtt-device-topic-id", DefaultTopicPrefix, "The device MQTT topic identifier")
	runCmd.Flags().BoolVar(&config.RunOnce, "once", false, "Only run the monitor once")
	runCmd.Flags().String("device-id", "", "thin-edge.io device id")

	runCmd.Flags().Duration("interval", 300*time.Second, "Metrics update interval")

	//
	// viper bindings

	// Service
	viper.SetDefault("monitor.service_name", DefaultServiceName)
	viper.BindPFlag("monitor.service_name", runCmd.Flags().Lookup("service-name"))

	// MQTT topics
	viper.SetDefault("monitor.mqtt.topic_root", DefaultTopicRoot)
	viper.BindPFlag("monitor.mqtt.topic_root", runCmd.Flags().Lookup("mqtt-topic-root"))
	viper.SetDefault("monitor.mqtt.device_topic_id", DefaultTopicPrefix)
	viper.BindPFlag("monitor.mqtt.device_topic_id", runCmd.Flags().Lookup("mqtt-device-topic-id"))
	viper.BindPFlag("monitor.mqtt.device_id", runCmd.Flags().Lookup("device-id"))

	// Include filters
	viper.BindPFlag("monitor.filter.include.names", runCmd.Flags().Lookup("name"))
	viper.BindPFlag("monitor.filter.include.labels", runCmd.Flags().Lookup("label"))
	viper.BindPFlag("monitor.filter.include.ids", runCmd.Flags().Lookup("id"))
	viper.BindPFlag("monitor.filter.include.types", runCmd.Flags().Lookup("type"))

	// Exclude filters
	viper.SetDefault("monitor.filter.exclude.names", "")
	viper.SetDefault("monitor.filter.exclude.labels", "")

	// Metrics
	viper.BindPFlag("monitor.metrics.interval", runCmd.Flags().Lookup("interval"))
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
}
