/*
Copyright © 2024 thin-edge.io <info@thin-edge.io>
*/
package cmd

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// Build data
var buildVersion string
var buildBranch string

var rootConfig RootConfig

type RootConfig struct {
	ConfigFile string
}

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "tedge-container-monitor",
	Short: "A brief description of your application",
	Long: `A longer description that spans multiple lines and likely contains
examples and usage of using your application. For example:

Cobra is a CLI library for Go that empowers applications.
This application is a tool to generate the needed files
to quickly create a Cobra application.`,
	Version: fmt.Sprintf("%s (branch=%s)", buildVersion, buildBranch),
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		return SetLogLevel()
	},
}

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() {
	args := os.Args
	name := filepath.Base(args[0])
	switch name {
	case "container", "container-group":
		slog.Debug("Calling as a software management plugin.", "name", name, "args", args)
		rootCmd.SetArgs(append([]string{name}, args[1:]...))
	default:
		slog.Debug("Using subcommands.", "args", args)
	}

	err := rootCmd.Execute()
	if err != nil {
		os.Exit(1)
	}
}

func SetLogLevel() error {
	value := strings.ToLower(viper.GetString("log_level"))
	slog.Info("Setting log level.", "new", value)
	switch value {
	case "info":
		slog.SetLogLoggerLevel(slog.LevelInfo)
	case "debug":
		slog.SetLogLoggerLevel(slog.LevelDebug)
	case "warn":
		slog.SetLogLoggerLevel(slog.LevelWarn)
	case "error":
		slog.SetLogLoggerLevel(slog.LevelError)
	}
	return nil
}

func initConfig() {
	if rootConfig.ConfigFile != "" {
		// Use config file from the flag.
		viper.SetConfigFile(rootConfig.ConfigFile)
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

func init() {
	cobra.OnInitialize(initConfig)
	rootCmd.PersistentFlags().String("log-level", "info", "Log level")
	rootCmd.PersistentFlags().StringVarP(&rootConfig.ConfigFile, "config", "c", "", "Configuration file")

	// viper.Bind
	viper.BindPFlag("log_level", rootCmd.PersistentFlags().Lookup("log-level"))
}
