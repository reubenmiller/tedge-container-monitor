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
	"github.com/thin-edge/tedge-container-monitor/cli/container"
	"github.com/thin-edge/tedge-container-monitor/cli/container_group"
	"github.com/thin-edge/tedge-container-monitor/cli/engine"
	"github.com/thin-edge/tedge-container-monitor/cli/run"
	"github.com/thin-edge/tedge-container-monitor/pkg/cli"
)

// Build data
var buildVersion string
var buildBranch string

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
		switch err.(type) {
		case cli.SilentError:
			// Don't log error
		default:
			slog.Error("Command error", "err", err)
		}
		os.Exit(1)
	}
}

func SetLogLevel() error {
	value := strings.ToLower(viper.GetString("log_level"))
	slog.Debug("Setting log level.", "new", value)
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

func init() {
	cliConfig := cli.Cli{}
	cobra.OnInitialize(cliConfig.OnInit)
	rootCmd.AddCommand(
		container.NewContainerCommand(cliConfig),
		container_group.NewContainerGroupCommand(cliConfig),
		run.NewRunCommand(cliConfig),
		engine.NewCliCommand(cliConfig),
	)

	rootCmd.PersistentFlags().String("log-level", "info", "Log level")
	rootCmd.PersistentFlags().StringVarP(&cliConfig.ConfigFile, "config", "c", "", "Configuration file")

	// viper.Bind
	_ = viper.BindPFlag("log_level", rootCmd.PersistentFlags().Lookup("log-level"))
}
