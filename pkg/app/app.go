package app

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/docker/docker/api/types/events"
	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/thin-edge/tedge-container-monitor/pkg/container"
	"github.com/thin-edge/tedge-container-monitor/pkg/tedge"
)

type App struct {
	client          *tedge.Client
	ContainerClient *container.ContainerClient

	Device *tedge.Target

	config         Config
	shutdown       chan struct{}
	updateRequests chan container.FilterOptions
	updateResults  chan error
	wg             sync.WaitGroup
}

type Config struct {
	ServiceName string

	// Feature flags
	EnableMetrics      bool
	EnableEngineEvents bool
	DeleteFromCloud    bool
}

func NewApp(device tedge.Target, config Config) (*App, error) {
	serviceTarget := device.Service(config.ServiceName)
	tedgeOpts := tedge.NewClientConfig()
	tedgeClient := tedge.NewClient(device, *serviceTarget, config.ServiceName, tedgeOpts)

	containerClient, err := container.NewContainerClient()
	if err != nil {
		return nil, err
	}

	if err := tedgeClient.Connect(); err != nil {
		return nil, err
	}

	if tedgeClient.Target.CloudIdentity == "" {
		for {
			slog.Info("Looking up thin-edge.io Cumulocity ExternalID")
			if currentUser, _, err := tedgeClient.CumulocityClient.User.GetCurrentUser(context.Background()); err == nil {
				externalID := strings.TrimPrefix(currentUser.Username, "device_")
				tedgeClient.Target.CloudIdentity = externalID
				device.CloudIdentity = externalID
				slog.Info("Found Cumulocity ExternalID", "value", tedgeClient.Target.CloudIdentity)
				break
			} else {
				slog.Warn("Failed to lookup Cumulocity ExternalID.", "err", err)
				// retry until it is successful
				time.Sleep(10 * time.Second)
			}
		}
	}

	application := &App{
		client:          tedgeClient,
		ContainerClient: containerClient,
		Device:          &device,
		config:          config,
		updateRequests:  make(chan container.FilterOptions),
		updateResults:   make(chan error),
		shutdown:        make(chan struct{}),
		wg:              sync.WaitGroup{},
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
				opts := container.FilterOptions{}
				// If the name matches the current service name, then
				// update all containers
				if name != a.config.ServiceName {
					opts.Names = []string{
						fmt.Sprintf("^%s$", name),
					}
				}
				a.updateRequests <- opts
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

var ContainerEventText = map[events.Action]string{
	events.ActionCreate:  "created",
	events.ActionStart:   "started",
	events.ActionStop:    "stopped",
	events.ActionDestroy: "destroyed",
	events.ActionRemove:  "removed",
	events.ActionDie:     "died",
	events.ActionPause:   "paused",
	events.ActionUnPause: "unpaused",
}

func mustMarshalJSON(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}

func (a *App) Monitor(ctx context.Context, filterOptions container.FilterOptions) error {
	evtCh, errCh := a.ContainerClient.MonitorEvents(ctx)

	for {
		select {
		case <-ctx.Done():
			slog.Info("Stopping engine monitor")
			return ctx.Err()
		case evt := <-evtCh:
			switch evt.Type {
			case events.ContainerEventType:
				payload := make(map[string]any)
				if v, ok := ContainerEventText[evt.Action]; ok {
					payload["text"] = fmt.Sprintf("%s %s", "container", v)
					payload["containerID"] = evt.Actor.ID
					payload["attributes"] = evt.Actor.Attributes
				}

				switch evt.Action {
				case events.ActionCreate:
					slog.Info("Container created", "container", evt.Actor.ID)
				case events.ActionStart, events.ActionStop, events.ActionPause, events.ActionUnPause:
					a.Update(container.FilterOptions{
						IDs: []string{evt.Actor.ID},
					})
				case events.ActionDestroy, events.ActionRemove:
					slog.Info("Container removed/destroyed", "container", evt.Actor.ID, "attributes", evt.Actor.Attributes)
					// TODO: Trigger a removal instead of checking the whole state
					// TODO: Lookup container name by container id (from the entity store) as lookup by name won't work for container-groups
					a.Update(container.FilterOptions{})
					// if containerName, ok := evt.Actor.Attributes["name"]; ok {
					// 	a.Deregister(containerName)
					// }
				}

				if a.config.EnableEngineEvents {
					if len(payload) > 0 {
						if err := a.client.Publish(tedge.GetTopic(a.client.Target, "e", string(evt.Action)), 1, false, mustMarshalJSON(payload)); err != nil {
							slog.Warn("Failed to publish container event.", "err", err)
						}
					}
				}
			}

			slog.Info("Received event.", "value", evt)
		case err := <-errCh:
			slog.Info("Received error.", "value", err)
		}
	}
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
	existingServices := make(map[string]struct{})
	for k, v := range entities {
		if v.(map[string]any)["type"] == container.ContainerType || v.(map[string]any)["type"] == container.ContainerGroupType {
			existingServices[k] = struct{}{}
		}
	}
	slog.Info("Found entities.", "total", len(entities))
	for key := range entities {
		slog.Debug("Entity store.", "key", key)
	}

	slog.Info("Reading containers")
	items, err := a.ContainerClient.List(context.Background(), filterOptions)
	if err != nil {
		return err
	}

	// Register devices
	slog.Info("Registering containers")
	for _, item := range items {
		target := a.Device.Service(item.Name)

		// Skip registration message if it already exists
		if _, ok := existingServices[target.Topic()]; ok {
			slog.Debug("Container is already registered", "topic", target.Topic())
			delete(existingServices, target.Topic())
			continue
		}
		delete(existingServices, target.Topic())

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
	if a.config.EnableMetrics {
		for _, item := range items {
			stats, err := a.ContainerClient.GetStats(context.Background(), item.Container.Id)
			if err != nil {
				slog.Warn("Failed to read container stats", "err", err)
			} else {
				slog.Info("Container stats.", "stats", stats)
			}
		}
	}

	// Delete removed values, via MQTT and c8y API
	markedForDeletion := make([]tedge.Target, 0)
	if removeStaleServices {
		slog.Info("Checking for any stale services")
		for staleTopic := range existingServices {
			slog.Info("Removing stale service", "topic", staleTopic)
			target, err := tedge.NewTargetFromTopic(staleTopic)
			if err != nil {
				slog.Warn("Invalid topic structure", "err", err)
				continue
			}

			// FIXME: Check if sending an empty retain message to the twin topic will recreate
			if err := tedgeClient.Publish(tedge.GetTopic(*target, "twin", "container"), 1, true, ""); err != nil {
				return err
			}
			if err := tedgeClient.DeregisterEntity(*target); err != nil {
				slog.Warn("Failed to deregister entity.", "err", err)
			}

			// mark targets for deletion from the cloud, but don't delete them yet to give time
			// for thin-edge.io to process the status updates
			markedForDeletion = append(markedForDeletion, *target)
		}

		// Delete cloud
		if len(markedForDeletion) > 0 {
			// Delay before deleting messages
			time.Sleep(500 * time.Millisecond)
			for _, target := range markedForDeletion {
				slog.Info("Removing service from the cloud", "topic", target.Topic())

				// FIXME: How to handle if the device is deregistered locally, but still exists in the cloud?
				// Should it try to reconcile with the cloud to delete orphaned services?
				// Delete service directly from Cumulocity using the local Cumulocity Proxy
				target.CloudIdentity = tedgeClient.Target.CloudIdentity
				if target.CloudIdentity != "" {
					// Delay deleting the value
					if _, err := tedgeClient.DeleteCumulocityManagedObject(target); err != nil {
						slog.Warn("Failed to delete managed object.", "err", err)
					}
				}
			}
		}
	}

	return nil
}

func (a *App) Deregister(name string) error {
	slog.Info("Removing service", "name", name)
	target := a.Device.Service(name)

	// FIXME: Check if sending an empty retain message to the twin topic will recreate
	if err := a.client.Publish(tedge.GetTopic(*target, "twin", "container"), 1, true, ""); err != nil {
		return err
	}
	if err := a.client.DeregisterEntity(*target); err != nil {
		slog.Warn("Failed to deregister entity.", "err", err)
	}

	if a.config.DeleteFromCloud {
		// Delay before deleting messages
		time.Sleep(500 * time.Millisecond)
		slog.Info("Removing service from the cloud")

		// FIXME: How to handle if the device is deregistered locally, but still exists in the cloud?
		// Should it try to reconcile with the cloud to delete orphaned services?
		// Delete service directly from Cumulocity using the local Cumulocity Proxy
		target.CloudIdentity = a.client.Target.CloudIdentity
		if target.CloudIdentity != "" {
			// Delay deleting the value
			if _, err := a.client.DeleteCumulocityManagedObject(*target); err != nil {
				slog.Warn("Failed to delete managed object.", "err", err)
			}
		}
	}
	return nil
}
