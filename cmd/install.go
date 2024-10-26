/*
Copyright © 2024 thin-edge.io <info@thin-edge.io>
*/
package cmd

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"

	containerSDK "github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/errdefs"
	"github.com/spf13/cobra"
	"github.com/thin-edge/tedge-container-monitor/pkg/container"
)

var installCmdOptions installOptions

var DefaultNetworkName string = "tedge"

type installOptions struct {
	ModuleVersion string
	File          string
}

// installCmd represents the install command
var installCmd = &cobra.Command{
	Use:   "install <MODULE_NAME>",
	Short: "Install/run a container",
	Long:  `Install/run a container`,
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		slog.Info("Executing", "cmd", cmd.CalledAs(), "args", args)
		containerName := args[0]
		imageRef := installCmdOptions.ModuleVersion
		if installCmdOptions.File != "" {
			return fmt.Errorf("TODO: not implemented")
		}

		cli, err := container.NewContainerClient()
		if err != nil {
			return err
		}

		ctx := context.Background()

		// Install shared network
		netwResp, err := cli.Client.NetworkCreate(ctx, DefaultNetworkName, network.CreateOptions{})

		if errdefs.IsConflict(err) {
			slog.Info("Network already exists.", "id", DefaultNetworkName)
		} else {
			if err != nil {
				return nil
			}
		}
		if netwResp.ID != "" {
			slog.Info("Create default network.", "id", netwResp.ID)
		}

		images, err := cli.Client.ImageList(ctx, image.ListOptions{
			Filters: filters.NewArgs(filters.Arg("reference", imageRef)),
		})
		if err != nil {
			return err
		}

		if len(images) == 0 {
			slog.Info("Pulling image.", "ref", imageRef)
			out, err := cli.Client.ImagePull(ctx, imageRef, image.PullOptions{})
			if err != nil {
				return err
			}
			defer out.Close()
			io.Copy(os.Stderr, out)
		} else {
			slog.Info("Image already exists.", "ref", imageRef, "id", images[0].ID, "tags", images[0].RepoTags)
		}

		containerConfig := &containerSDK.Config{
			Image:  imageRef,
			Labels: map[string]string{},
		}

		resp, err := cli.Client.ContainerCreate(
			ctx,
			containerConfig,
			&containerSDK.HostConfig{
				RestartPolicy: containerSDK.RestartPolicy{
					Name:              containerSDK.RestartPolicyOnFailure,
					MaximumRetryCount: 5,
				},
			},
			&network.NetworkingConfig{
				EndpointsConfig: map[string]*network.EndpointSettings{
					DefaultNetworkName: {
						NetworkID: DefaultNetworkName,
					},
				},
			},
			nil,
			containerName,
		)
		if err != nil {
			return err
		}

		if err := cli.Client.ContainerStart(ctx, resp.ID, containerSDK.StartOptions{}); err != nil {
			return err
		}

		slog.Info("created container.", "id", resp.ID, "name", containerName)
		return nil
	},
}

func init() {
	containerCmd.AddCommand(installCmd)

	// Here you will define your flags and configuration settings.
	// installCmd.
	installCmd.Flags().StringVar(&installCmdOptions.ModuleVersion, "module-version", "", "Software version to install")
	installCmd.Flags().StringVar(&installCmdOptions.File, "file", "", "File")
}
