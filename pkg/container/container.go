package container

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	"github.com/docker/go-units"
)

var ContainerType string = "container"
var ContainerGroupType string = "container-group"

func NewJSONTime(t time.Time) JSONTime {
	return JSONTime{
		Time: t,
	}
}

type JSONTime struct {
	time.Time
	AsRFC3339 bool
}

func (t JSONTime) MarshalJSON() ([]byte, error) {
	if t.AsRFC3339 {
		v := fmt.Sprintf("\"%s\"", time.Time(t.Time).Format(time.RFC3339))
		return []byte(v), nil
	}
	v := fmt.Sprintf("%d", t.Time.Unix())
	return []byte(v), nil
}

func (t *JSONTime) UnmarshalJSON(data []byte) error {
	var tmpValue any
	if err := json.Unmarshal(data, tmpValue); err != nil {
		return err
	}

	switch value := tmpValue.(type) {
	case int32:
		t.Time = time.Unix(int64(value), 0)
	case int64:
		t.Time = time.Unix(value, 0)
	case float64:
		sec, dec := math.Modf(value)
		t.Time = time.Unix(int64(sec), int64(dec*(1e9)))
	case string:
		v, err := time.Parse(time.RFC3339Nano, value)
		if err != nil {
			return err
		}
		t.Time = v
	default:
		return fmt.Errorf("invalid format. only Unix timestamp or RFC3339 formats are supported")
	}

	return nil
}

type TedgeContainer struct {
	Name        string    `json:"name"`
	Status      string    `json:"status"`
	ServiceType string    `json:"serviceType"`
	Container   Container `json:"container"`
	Time        JSONTime  `json:"time"`
}

type Container struct {
	Name        string   `json:"-"`
	Id          string   `json:"containerId,omitempty"`
	State       string   `json:"state,omitempty"`
	Status      string   `json:"containerStatus,omitempty"`
	CreatedAt   string   `json:"createdAt,omitempty"`
	Image       string   `json:"image,omitempty"`
	Ports       string   `json:"ports,omitempty"`
	NetworkIDs  []string `json:"-"`
	Networks    string   `json:"networks,omitempty"`
	RunningFor  string   `json:"runningFor,omitempty"`
	Filesystem  string   `json:"filesystem,omitempty"`
	Command     string   `json:"command,omitempty"`
	NetworkMode string   `json:"networkMode,omitempty"`

	// Only used for container groups
	ServiceName string `json:"serviceName,omitempty"`
	ProjectName string `json:"projectName,omitempty"`
}

func NewContainerFromDockerContainer(item *types.Container) Container {
	container := Container{
		Id:          item.ID,
		Name:        ConvertName(item.Names),
		State:       item.State,
		Status:      item.Status,
		Image:       item.Image,
		Command:     item.Command,
		CreatedAt:   time.Unix(item.Created, 0).Format(time.RFC3339),
		Ports:       FormatPorts(item.Ports),
		NetworkMode: item.HostConfig.NetworkMode,
	}

	// Mimic filesystem
	srw := units.HumanSizeWithPrecision(float64(item.SizeRw), 3)
	sv := units.HumanSizeWithPrecision(float64(item.SizeRootFs), 3)
	container.Filesystem = srw
	if item.SizeRootFs > 0 {
		container.Filesystem = fmt.Sprintf("%s (virtual %s)", srw, sv)
	}

	if v, ok := item.Labels["com.docker.compose.project"]; ok {
		container.ProjectName = v
	}

	if v, ok := item.Labels["com.docker.compose.service"]; ok {
		container.ServiceName = v
	}

	container.NetworkIDs = make([]string, 0)
	if item.NetworkSettings != nil && len(item.NetworkSettings.Networks) > 0 {
		for _, v := range item.NetworkSettings.Networks {
			container.NetworkIDs = append(container.NetworkIDs, v.NetworkID)
		}
	}
	return container
}

func (c *Container) GetName() string {
	if c.ProjectName == "" {
		return c.Name
	}
	return fmt.Sprintf("%s@%s", c.ProjectName, c.ServiceName)
}

func ConvertToTedgeStatus(v string) string {
	switch v {
	case "up", "running":
		return "up"
	default:
		return "down"
	}
}

func FormatPorts(values []types.Port) string {
	formatted := make([]string, 0, len(values))
	for _, port := range values {
		if port.PublicPort == 0 {
			formatted = append(formatted, fmt.Sprintf("%d/%s", port.PrivatePort, port.Type))
		} else {
			if port.IP == "" {
				formatted = append(formatted, fmt.Sprintf("%d:%d/%s", port.PublicPort, port.PrivatePort, port.Type))
			} else {
				formatted = append(formatted, fmt.Sprintf("%s:%d:%d/%s", port.IP, port.PublicPort, port.PrivatePort, port.Type))
			}
		}
	}
	return strings.Join(formatted, ", ")
}

func ConvertName(v []string) string {
	return strings.TrimPrefix(v[0], "/")
}

type ContainerClient struct {
	Client *client.Client
}

func NewContainerClient() (*ContainerClient, error) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, err
	}
	return &ContainerClient{
		Client: cli,
	}, nil
}

type ContainerTelemetryMessage struct {
	Container ContainerStats `json:"container"`
}

type ContainerStats struct {
	Cpu    float32 `json:"cpu"`
	Memory float32 `json:"memory"`
	NetIO  float32 `json:"netio"`
}

func (c *ContainerClient) GetStats(ctx context.Context, containerID string) (map[string]any, error) {
	resp, err := c.Client.ContainerStatsOneShot(ctx, containerID)
	if err != nil {
		return nil, err
	}

	stats := make(map[string]any)
	decode := json.NewDecoder(resp.Body)
	err = decode.Decode(&stats)

	// b, err := io.ReadAll(resp.Body)
	// if err != nil {
	// 	return nil, err
	// }
	// res := gjson.ParseBytes(b)

	// See https://github.com/docker/cli/blob/master/cli/command/container/stats_helpers.go#L105
	// https://github.com/docker/cli/blob/062eecf14af34d7295da16c23c2578fcf4aa0196/cli/command/container/stats_helpers.go#L70
	// ContainerTelemetryMessage{
	// 	Container: ContainerStats{
	// 		Cpu: res.Get("MemoryStats.Limit"),
	// 		Memory: res.Get("MemoryStats.Limit"),
	// 	},
	// }
	return stats, err
}

func (c *ContainerClient) List(ctx context.Context) ([]TedgeContainer, error) {

	// "com.docker.compose"
	// filters := filters.NewArgs(filters.KeyValuePair{
	// 	Key:   "label",
	// 	Value: "com.docker.compose",
	// })

	// Filter for docker compose projects
	// filters := filters.NewArgs(filters.Arg("label", "com.docker.compose.project"))

	timestamp := JSONTime{
		Time: time.Now(),
	}
	containers, err := c.Client.ContainerList(ctx, container.ListOptions{
		Size: true,
		// Filters: filters,
	})
	if err != nil {
		return nil, err
	}

	// TODO: Is this needed, as the docker ps -a list seems to list the NetworkMode as the "network"
	networks, err := c.Client.NetworkList(ctx, network.ListOptions{})
	if err != nil {
		return nil, err
	}
	networkIndex := make(map[string]int)
	for i, netw := range networks {
		networkIndex[netw.ID] = i
	}

	items := make([]TedgeContainer, 0, len(containers))
	for _, i := range containers {
		container := NewContainerFromDockerContainer(&i)
		item := TedgeContainer{
			Name:        container.GetName(),
			Time:        timestamp,
			Status:      ConvertToTedgeStatus(i.State),
			ServiceType: ContainerType,
			Container:   container,
		}

		for _, netID := range container.NetworkIDs {
			if netIdx, ok := networkIndex[netID]; ok {
				item.Container.Networks = networks[netIdx].Name
				break
			}
		}

		// Ignore docker compose projects
		if _, ok := i.Labels["com.docker.compose.project"]; ok {
			item.ServiceType = ContainerGroupType
		}

		items = append(items, item)
	}

	return items, nil
}
