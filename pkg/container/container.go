package container

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"os"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/events"
	"github.com/docker/docker/api/types/filters"
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

	// Private values
	Labels map[string]string `json:"-"`
}

func NewContainerFromDockerContainer(item *types.Container) TedgeContainer {
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
		Labels:      item.Labels,
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

	containerType := ContainerType
	// Set service type. A docker compose project is a "container-group"
	if _, ok := item.Labels["com.docker.compose.project"]; ok {
		containerType = ContainerGroupType
	}

	return TedgeContainer{
		Name: container.GetName(),
		Time: JSONTime{
			Time: time.Now(),
		},
		Status:      ConvertToTedgeStatus(item.State),
		ServiceType: containerType,
		Container:   container,
	}
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

func socketExists(p string) bool {
	_, err := os.Stat(strings.TrimPrefix(p, "unix://"))
	return err == nil
}

func findContainerEngineSocket() (socketAddr string) {
	containerSockets := []string{
		"unix:///run/podman/podman.sock",
	}

	for _, addr := range containerSockets {
		if strings.HasPrefix(addr, "unix://") {
			if socketExists(addr) {
				socketAddr = addr
				break
			}
		}
	}
	return socketAddr
}

func NewContainerClient() (*ContainerClient, error) {
	// Find container socket
	if v := os.Getenv("DOCKER_HOST"); v == "" {
		if addr := findContainerEngineSocket(); addr != "" {
			if err := os.Setenv("DOCKER_HOST", addr); err != nil {
				return nil, err
			}
			slog.Info("Using container engine socket.", "value", addr)
		}
	}

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
	Cpu    uint64 `json:"cpu"`
	Memory uint64 `json:"memory"`
	NetIO  uint64 `json:"netio"`
}

func (c *ContainerClient) GetStats(ctx context.Context, containerID string) (*ContainerTelemetryMessage, error) {
	wg := sync.WaitGroup{}
	wg.Add(1)
	containerStats := &Stats{
		StatsEntry: StatsEntry{
			Container: containerID,
		},
	}

	// Start collecting statistics
	collect(ctx, containerStats, c.Client, false, &wg)
	wg.Wait()

	s := containerStats.GetStatistics()
	slog.Info("Stats.", "memPerc", s.MemoryPercentage, "cpuPerc", s.CPUPercentage, "networkIO", s.NetworkTx)

	resp, err := c.Client.ContainerStatsOneShot(ctx, containerID)
	if err != nil {
		return nil, err
	}

	statsResponse := &container.StatsResponse{}
	dec := json.NewDecoder(resp.Body)
	if err := dec.Decode(&statsResponse); err != nil {
		return nil, err
	}

	// See https://github.com/docker/cli/blob/master/cli/command/container/stats_helpers.go#L105
	// https://github.com/docker/cli/blob/062eecf14af34d7295da16c23c2578fcf4aa0196/cli/command/container/stats_helpers.go#L70
	// https://stackoverflow.com/questions/30271942/get-docker-container-cpu-usage-as-percentage
	txBytes := uint64(0)
	for _, netw := range statsResponse.Networks {
		txBytes += netw.TxBytes
	}

	stats := &ContainerTelemetryMessage{
		Container: ContainerStats{
			Cpu:    statsResponse.CPUStats.SystemUsage,
			Memory: statsResponse.MemoryStats.Usage,
			NetIO:  txBytes,
		},
	}

	return stats, err
}

type FilterOptions struct {
	Names  []string
	Labels []string
	IDs    []string

	// Client side filters
	Types            []string
	ExcludeNames     []string
	ExcludeWithLabel []string
}

func (fo FilterOptions) IsEmpty() bool {
	return len(fo.Names) == 0 && len(fo.Labels) == 0 && len(fo.IDs) == 0
}

func (c *ContainerClient) GetContainer(ctx context.Context, containerID string) (*TedgeContainer, error) {
	containers, err := c.List(ctx, FilterOptions{
		IDs: []string{containerID},
	})
	if err != nil {
		return nil, err
	}
	if len(containers) == 0 {
		return nil, fmt.Errorf("container not found")
	}
	return &containers[0], nil
}

func (c *ContainerClient) List(ctx context.Context, options FilterOptions) ([]TedgeContainer, error) {
	// Filter for docker compose projects
	listOptions := container.ListOptions{
		Size: true,
		All:  true,
	}

	filterValues := make([]filters.KeyValuePair, 0)

	// Match by container name
	for _, name := range options.Names {
		filterValues = append(filterValues, filters.KeyValuePair{
			Key:   "name",
			Value: name,
		})
	}

	// Match by container id
	for _, value := range options.IDs {
		filterValues = append(filterValues, filters.KeyValuePair{
			Key:   "id",
			Value: value,
		})
	}

	// filterValues = append(filterValues, filters.Arg("label", "com.docker.compose.project"))

	// Match by label
	for _, label := range options.Labels {
		filterValues = append(filterValues, filters.KeyValuePair{
			Key:   "label",
			Value: label,
		})
	}

	if len(filterValues) > 0 {
		listOptions.Filters = filters.NewArgs(filterValues...)
	}

	containers, err := c.Client.ContainerList(ctx, listOptions)
	if err != nil {
		return nil, err
	}

	// Pre-compile regular expressions
	excludeNamesRegex := make([]regexp.Regexp, 0, len(options.ExcludeNames))
	for _, pattern := range options.ExcludeNames {
		if p, err := regexp.Compile(pattern); err != nil {
			slog.Warn("Invalid excludeNames regex pattern.", "pattern", pattern, "err", err)
		} else {
			excludeNamesRegex = append(excludeNamesRegex, *p)
		}
	}

	items := make([]TedgeContainer, 0, len(containers))
	for _, i := range containers {
		item := NewContainerFromDockerContainer(&i)

		// Apply client side filters
		if len(options.Types) > 0 {
			if !slices.Contains(options.Types, item.ServiceType) {
				continue
			}
		}

		if len(excludeNamesRegex) > 0 {
			ignoreContainer := false
			for _, pattern := range excludeNamesRegex {
				if pattern.MatchString(item.Container.Name) || pattern.MatchString(item.Name) {
					ignoreContainer = true
					break
				}
			}
			if ignoreContainer {
				continue
			}
		}

		if len(options.ExcludeWithLabel) > 0 {
			hasLabel := false
			for _, label := range options.ExcludeWithLabel {
				if _, hasLabel = item.Container.Labels[label]; hasLabel {
					break
				}
			}
			if hasLabel {
				continue
			}
		}
		items = append(items, item)
	}

	return items, nil
}

func (c *ContainerClient) MonitorEvents(ctx context.Context) (<-chan events.Message, <-chan error) {
	return c.Client.Events(context.Background(), events.ListOptions{})
}
