/*
Copyright © 2024 thin-edge.io <info@thin-edge.io>
*/
package cmd

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"strings"

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

type ImageResponse struct {
	Stream string `json:"stream"`
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

		cli, err := container.NewContainerClient()
		if err != nil {
			return err
		}

		ctx := context.Background()

		if installCmdOptions.File != "" {
			slog.Info("Loading image from file.", "file", installCmdOptions.File)
			file, err := os.Open(installCmdOptions.File)
			if err != nil {
				return err
			}

			imageResp, err := cli.Client.ImageLoad(ctx, file, true)
			if err != nil {
				return err
			}
			defer imageResp.Body.Close()
			if imageResp.JSON {
				b, err := io.ReadAll(imageResp.Body)
				if err != nil {
					return nil
				}
				imageDetails := &ImageResponse{}
				if err := json.Unmarshal(b, &imageDetails); err != nil {
					return err
				}

				if strings.HasPrefix(imageDetails.Stream, "Loaded image: ") {
					imageRef = strings.TrimPrefix(imageDetails.Stream, "Loaded image: ")
					slog.Info("Using imageRef from loaded image.", "name", imageRef)
				}
				slog.Info("Loaded image.", "stream", imageDetails.Stream)
			}
		}

		// Install shared network
		netw, err := cli.Client.NetworkInspect(ctx, DefaultNetworkName, network.InspectOptions{})
		if err != nil {
			if !errdefs.IsNotFound(err) {
				return err
			}
			// Create network
			netwResp, err := cli.Client.NetworkCreate(ctx, DefaultNetworkName, network.CreateOptions{})
			if err != nil {
				return err
			}
			slog.Info("Created network.", "name", DefaultNetworkName, "id", netwResp.ID)
		} else {
			// Network already exists
			slog.Info("Network already exists.", "name", netw.Name, "id", netw.ID)
		}
		// netwResp, err := cli.Client.NetworkCreate(ctx, DefaultNetworkName, network.CreateOptions{})

		// if errdefs.IsConflict(err) {
		// 	slog.Info("Network already exists.", "id", DefaultNetworkName)
		// } else if err != nil {
		// 	slog.Error("Failed to create container network.", "err", err)
		// 	return err
		// }
		// if netwResp.ID != "" {
		// 	slog.Info("Create default network.", "id", netwResp.ID)
		// }

		//
		// Check and pull image if it is not present
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

		//
		// Stop/remove any existing images with the same name
		if err := cli.StopRemoveContainer(ctx, containerName); err != nil {
			slog.Warn("Could not stop and remove the existing container.", "err", err)
			return err
		}

		//
		// Create new container
		containerConfig := &containerSDK.Config{
			Image:  imageRef,
			Labels: map[string]string{},
		}

		resp, err := cli.Client.ContainerCreate(
			ctx,
			containerConfig,
			&containerSDK.HostConfig{
				PublishAllPorts: true,
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
