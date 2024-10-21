package app

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"

	"github.com/thin-edge/tedge-container-monitor/pkg/container"
	"github.com/thin-edge/tedge-container-monitor/pkg/tedge"
)

type App struct {
	client *tedge.Client

	Device *tedge.Target
}

func NewApp(device tedge.Target, serviceName string) (*App, error) {
	serviceTarget := device.Service(serviceName)
	tedgeClient := tedge.NewClient(*serviceTarget, serviceName, tedge.NewClientConfig())

	if err := tedgeClient.Connect(); err != nil {
		return nil, err
	}

	if tedgeClient.Target.CloudIdentity == "" {
		slog.Info("Looking up thin-edge.io Cumulocity ExternalID")
		if currentUser, _, err := tedgeClient.CumulocityClient.User.GetCurrentUser(context.Background()); err == nil {
			tedgeClient.Target.CloudIdentity = strings.TrimPrefix(currentUser.Username, "device_")
			slog.Info("Found Cumulocity ExternalID", "value", tedgeClient.Target.CloudIdentity)
		}
	}

	application := &App{
		client: tedgeClient,
		Device: &device,
	}
	return application, nil
}

func (a *App) Update() error {
	tedgeClient := a.client
	entities, err := tedgeClient.GetEntities()
	if err != nil {
		return err
	}

	// Record all registered services
	staleServices := make(map[string]struct{})
	for k, v := range entities {
		if v.(map[string]any)["type"] == container.ContainerType || v.(map[string]any)["type"] == container.ContainerGroupType {
			staleServices[k] = struct{}{}
		}
	}
	slog.Info("Found entities", "total", len(entities))

	client, err := container.NewContainerClient()
	if err != nil {
		return err
	}

	slog.Info("Reading containers")
	items, err := client.List(context.Background())
	if err != nil {
		return err
	}

	// Register devices
	slog.Info("Registering containers")
	for _, item := range items {
		target := a.Device.Service(item.Name)

		// Skip registration message if it already exists
		if _, ok := staleServices[target.Topic()]; ok {
			slog.Debug("Container is already registered", "topic", target.Topic())
			delete(staleServices, target.Topic())
			continue
		}
		delete(staleServices, target.Topic())

		payload := map[string]any{
			"@type": "service",
			"name":  item.Name,
			"type":  item.ServiceType,
		}
		b, err := json.Marshal(payload)
		if err != nil {
			slog.Warn("Could not marshal registration message", "err", err)
			continue
		}
		if err := tedgeClient.Publish(target.Topic(), 1, true, b); err != nil {
			slog.Error("Failed to register container", "target", target.Topic(), "err", err)
		}
	}

	// Publish health messages
	for _, item := range items {
		target := a.Device.Service(item.Name)

		payload := map[string]any{
			"status": item.Status,
			"time":   item.Time,
		}
		b, err := json.Marshal(payload)
		if err != nil {
			slog.Warn("Could not marshal registration message", "err", err)
			continue
		}
		topic := tedge.GetHealthTopic(*target)
		if err := tedgeClient.Publish(topic, 1, true, b); err != nil {
			slog.Error("Failed to update health status", "target", topic, "err", err)
		}
	}

	// update digital twin information
	slog.Info("Updating digital twin information")
	for _, item := range items {
		target := a.Device.Service(item.Name)

		topic := tedge.GetTopic(*target, "twin", "container")

		// Create status
		slog.Info("Publishing container status", "topic", topic)
		payload, err := json.Marshal(item.Container)

		if err != nil {
			slog.Error("Failed to convert payload to json", "err", err)
			continue
		}

		if err := tedgeClient.Publish(topic, 1, true, payload); err != nil {
			slog.Error("Could not publish container status", "err", err)
		}
	}

	// Update metrics
	for _, item := range items {
		stats, err := client.GetStats(context.Background(), item.Container.Id)
		if err != nil {
			slog.Warn("Failed to read container stats", "err", err)
		} else {
			slog.Info("Container stats.", "stats", stats)
		}
	}

	// Delete removed values, via MQTT and c8y API
	slog.Info("Removing any stale services")
	for staleTopic := range staleServices {
		slog.Info("Removing stale service", "topic", staleTopic)
		target, err := tedge.NewTargetFromTopic(staleTopic)
		if err != nil {
			slog.Warn("Invalid topic structure", "err", err)
			continue
		}

		if err := tedgeClient.Publish(tedge.GetTopic(*target, "twin", "container"), 1, true, ""); err != nil {
			return err
		}
		tedgeClient.DeregisterEntity(*target)

		// FIXME: How to handle if the device is deregistered locally, but still exists in the cloud?
		// Should it try to reconcile with the cloud to delete orphaned services?
		// Delete service directly from Cumulocity using the local Cumulocity Proxy
		target.CloudIdentity = tedgeClient.Target.CloudIdentity
		if target.CloudIdentity != "" {
			tedgeClient.DeleteCumulocityManagedObject(*target)
		}

	}
	return nil
}
