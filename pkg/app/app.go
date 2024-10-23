package app

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/thin-edge/tedge-container-monitor/pkg/container"
	"github.com/thin-edge/tedge-container-monitor/pkg/tedge"
)

type App struct {
	client *tedge.Client

	Device *tedge.Target

	shutdown       chan struct{}
	updateRequests chan container.FilterOptions
	updateResults  chan error
	wg             sync.WaitGroup
}

func NewApp(device tedge.Target, serviceName string) (*App, error) {
	serviceTarget := device.Service(serviceName)
	tedgeOpts := tedge.NewClientConfig()
	tedgeClient := tedge.NewClient(*serviceTarget, serviceName, tedgeOpts)

	if err := tedgeClient.Connect(); err != nil {
		return nil, err
	}

	if tedgeClient.Target.CloudIdentity == "" {
		slog.Info("Looking up thin-edge.io Cumulocity ExternalID")
		if currentUser, _, err := tedgeClient.CumulocityClient.User.GetCurrentUser(context.Background()); err == nil {
			tedgeClient.Target.CloudIdentity = strings.TrimPrefix(currentUser.Username, "device_")
			slog.Info("Found Cumulocity ExternalID", "value", tedgeClient.Target.CloudIdentity)
		} else {
			slog.Warn("Failed to lookup Cumulocity ExternalID.", "err", err)
		}
	}

	application := &App{
		client:         tedgeClient,
		Device:         &device,
		updateRequests: make(chan container.FilterOptions),
		updateResults:  make(chan error),
		shutdown:       make(chan struct{}),
		wg:             sync.WaitGroup{},
	}

	// Start background task to process requests
	application.wg.Add(1)
	go application.worker()

	return application, nil
}

func (a *App) Subscribe() error {
	topic := tedge.GetTopic(*a.Device.Service("+"), "cmd", "health", "check")
	slog.Info("Listening to commands on topic.", "topic", topic)

	a.client.Client.AddRoute(topic, func(c mqtt.Client, m mqtt.Message) {
		parts := strings.Split(m.Topic(), "/")
		if len(parts) > 5 {
			slog.Info("Received request to update service data.", "service", parts[4], "topic", topic)
			go func(name string) {
				a.updateRequests <- container.FilterOptions{
					Names: []string{
						fmt.Sprintf("^%s$", name),
					},
				}
			}(parts[4])
		}
	})

	return nil
}

func (a *App) Stop(clean bool) {
	if a.client != nil {
		if clean {
			slog.Info("Disconnecting MQTT client cleanly")
			a.client.Client.Disconnect(250)
		}
	}
	a.shutdown <- struct{}{}

	// Wait for shutdown confirmation
	a.wg.Wait()
}

func (a *App) worker() {
	defer a.wg.Done()
	for {
		select {
		case opts := <-a.updateRequests:
			slog.Info("Processing update request")
			err := a.doUpdate(opts)
			slog.Info("Result", "err", err)
			// Don't block when publishing results
			go func() {
				a.updateResults <- err
			}()
		case <-a.shutdown:
			slog.Info("Stopping background task")
			return
		}
	}
}

func (a *App) Update(filterOptions container.FilterOptions) error {
	a.updateRequests <- filterOptions
	err := <-a.updateResults
	return err
}

func (a *App) doUpdate(filterOptions container.FilterOptions) error {
	tedgeClient := a.client
	entities, err := tedgeClient.GetEntities()
	if err != nil {
		return err
	}

	// Don't remove stale services when doing client side filtering
	// as there is no clean way to tell
	removeStaleServices := filterOptions.IsEmpty()

	// Record all registered services
	staleServices := make(map[string]struct{})
	for k, v := range entities {
		if v.(map[string]any)["type"] == container.ContainerType || v.(map[string]any)["type"] == container.ContainerGroupType {
			staleServices[k] = struct{}{}
		}
	}
	slog.Info("Found entities.", "total", len(entities))
	for key := range entities {
		slog.Debug("Entity store.", "key", key)
	}

	client, err := container.NewContainerClient()
	if err != nil {
		return err
	}

	slog.Info("Reading containers")
	items, err := client.List(context.Background(), filterOptions)
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
	if removeStaleServices {
		slog.Info("Checking for any stale services")
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
			if err := tedgeClient.DeregisterEntity(*target); err != nil {
				slog.Warn("Failed to deregister entity.", "err", err)
			}

			// FIXME: How to handle if the device is deregistered locally, but still exists in the cloud?
			// Should it try to reconcile with the cloud to delete orphaned services?
			// Delete service directly from Cumulocity using the local Cumulocity Proxy
			target.CloudIdentity = tedgeClient.Target.CloudIdentity
			if target.CloudIdentity != "" {
				if _, err := tedgeClient.DeleteCumulocityManagedObject(*target); err != nil {
					slog.Warn("Failed to delete managed object.", "err", err)
				}
			}
		}
	}

	return nil
}
