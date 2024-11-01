package cli

import (
	"fmt"
	"log/slog"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/thin-edge/tedge-container-plugin/pkg/container"
	"github.com/thin-edge/tedge-container-plugin/pkg/tedge"
)

type SilentError error

type Cli struct {
	ConfigFile string
}

func (c *Cli) OnInit() {
	if c.ConfigFile != "" {
		// Use config file from the flag.
		viper.SetConfigFile(c.ConfigFile)
	} else {
		// Find home directory.
		home, err := os.UserHomeDir()
		cobra.CheckErr(err)

		// Search config in home directory with name ".cobra" (without extension).
		viper.AddConfigPath(home)
		viper.SetConfigType("yaml")
		viper.SetConfigName(".tedge-container")
	}

	viper.SetEnvPrefix("CONTAINER")
	viper.AutomaticEnv()
	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))

	if err := viper.ReadInConfig(); err == nil {
		slog.Info("Using config file", "path", viper.ConfigFileUsed())
	}
}

func (c *Cli) GetString(key string) string {
	return viper.GetString(key)
}

func (c *Cli) GetBool(key string) bool {
	return viper.GetBool(key)
}

func (c *Cli) PrintConfig() {
	keys := viper.AllKeys()
	sort.Strings(keys)
	for _, key := range keys {
		slog.Info("setting", "item", fmt.Sprintf("%s=%v", key, viper.Get(key)))
	}
}

func (c *Cli) GetServiceName() string {
	return viper.GetString("monitor.service_name")
}

func (c *Cli) GetKeyFile() string {
	return viper.GetString("monitor.client.key")
}

func (c *Cli) GetCertificateFile() string {
	return viper.GetString("monitor.client.cert_file")
}

func (c *Cli) GetCAFile() string {
	return viper.GetString("monitor.client.ca_file")
}

func (c *Cli) GetTopicRoot() string {
	return viper.GetString("monitor.mqtt.topic_root")
}

func (c *Cli) GetTopicID() string {
	return viper.GetString("monitor.mqtt.device_topic_id")
}

func (c *Cli) GetDeviceID() string {
	return viper.GetString("monitor.mqtt.device_id")
}

func (c *Cli) MetricsEnabled() bool {
	return viper.GetBool("monitor.metrics.enabled")
}

func (c *Cli) EngineEventsEnabled() bool {
	return viper.GetBool("monitor.events.enabled")
}

func (c *Cli) DeleteFromCloud() bool {
	return viper.GetBool("monitor.delete_from_cloud.enabled")
}

func (c *Cli) GetMQTTHost() string {
	return viper.GetString("monitor.mqtt.client.host")
}

func (c *Cli) GetMetricsInterval() time.Duration {
	interval := viper.GetDuration("monitor.metrics.interval")
	if interval < 60*time.Second {
		slog.Warn("monitor.metrics.interval is lower than allowed limit.", "old", interval, "new", 60*time.Second)
		interval = 60 * time.Second
	}
	return interval
}

func (c *Cli) GetMQTTPort() uint16 {
	v := viper.GetUint16("monitor.mqtt.client.port")
	if v == 0 {
		return 1883
	}
	return v
}

func (c *Cli) GetCumulocityHost() string {
	return viper.GetString("monitor.c8y.proxy.client.host")
}

func (c *Cli) GetCumulocityPort() uint16 {
	v := viper.GetUint16("monitor.c8y.proxy.client..port")
	if v == 0 {
		return 8001
	}
	return v
}

func (c *Cli) GetDeviceTarget() tedge.Target {
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

func (c *Cli) GetFilterOptions() container.FilterOptions {
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
