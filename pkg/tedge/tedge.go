package tedge

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/reubenmiller/go-c8y/pkg/c8y"
)

var StatusUp = "up"
var StatusDown = "down"
var StatusUnknown = "unknown"

func PayloadHealthStatusDown() []byte {
	return []byte(fmt.Sprintf(`{"status":"%s"}`, StatusDown))
}

func PayloadHealthStatus(payload map[string]any, status string) ([]byte, error) {
	payload["status"] = status
	payload["time"] = time.Now().Unix()
	b, err := json.Marshal(payload)
	return b, err
}

type Client struct {
	Client           mqtt.Client
	Target           Target
	CumulocityClient *c8y.Client
}

type ClientConfig struct {
	MqttHost string
	MqttPort uint16
	C8yHost  string
	C8yPort  uint16
}

func NewClientConfig() *ClientConfig {
	return &ClientConfig{
		MqttHost: "127.0.0.1",
		MqttPort: 1883,
		C8yHost:  "127.0.0.1",
		C8yPort:  8001,
	}
}

func NewClient(target Target, serviceName string, config *ClientConfig) *Client {
	opts := mqtt.NewClientOptions()
	opts.AddBroker(fmt.Sprintf("tcp://%s:%d", config.MqttHost, config.MqttPort))
	opts.ClientID = fmt.Sprintf("%s#%s", target.Topic(), serviceName)
	opts.CleanSession = true
	opts.Order = false
	opts.WillRetained = true
	opts.AutoReconnect = true
	opts.AutoAckDisabled = false
	opts.WillEnabled = true
	opts.WillQos = 1
	opts.WillPayload = PayloadHealthStatusDown()
	opts.WillTopic = GetHealthTopic(target)
	client := mqtt.NewClient(opts)

	// TODO: Read port and host from settings
	// TODO: Support local certificate based auth
	c8yURL := fmt.Sprintf("http://%s:%d/c8y", config.C8yHost, config.C8yPort)
	c8yclient := c8y.NewClient(nil, c8yURL, "", "", "", true)

	slog.Info("MQTT Client options.", "clientID", opts.ClientID)

	return &Client{
		Client:           client,
		Target:           target,
		CumulocityClient: c8yclient,
	}
}

// Connect the MQTT client to the thin-edge.io broker
func (c *Client) Connect() error {
	tok := c.Client.Connect()
	if !tok.WaitTimeout(30 * time.Second) {
		panic("Failed to connect to broker")
	}
	<-tok.Done()
	if err := tok.Error(); err != nil {
		return err
	}

	payload, err := PayloadHealthStatus(map[string]any{}, StatusUp)
	if err != nil {
		return err
	}
	tok = c.Client.Publish(GetTopicRegistration(c.Target), 1, true, payload)
	<-tok.Done()
	if err := tok.Error(); err != nil {
		return err
	}
	slog.Info("Registered service", "topic", GetTopicRegistration(c.Target))

	return nil
}

// Delete a Cumulocity Managed object by External ID
func (c *Client) DeleteCumulocityManagedObject(target Target) (bool, error) {
	extID, resp, err := c.CumulocityClient.Identity.GetExternalID(context.Background(), "c8y_Serial", target.ExternalID())

	switch resp.StatusCode() {
	case http.StatusNotFound:
		return false, nil
	default:
		slog.Warn("Failed to lookup external id", "err", err)
	}

	if _, err := c.CumulocityClient.Inventory.Delete(context.Background(), extID.ManagedObject.ID); err != nil {
		slog.Warn("Failed to delete service", "id", extID.ManagedObject.ID, "err", err)
		return false, err
	}
	return true, nil
}

// Publish an MQTT message
func (c *Client) Publish(topic string, qos byte, retained bool, payload any) error {
	tok := c.Client.Publish(topic, 1, retained, payload)
	if !tok.WaitTimeout(100 * time.Millisecond) {
		return fmt.Errorf("timed out")
	}
	return tok.Error()
}

// Deregister a thin-edge.io entity
// Clear the status health topic as well as the registration topic
func (c *Client) DeregisterEntity(target Target) error {
	if err := c.Publish(GetTopic(target, "status", "health"), 1, true, ""); err != nil {
		return err
	}

	if err := c.Publish(GetTopic(target), 1, true, ""); err != nil {
		return err
	}

	return nil
}

// Get the thin-edge.io entities that have already been registered (as retained messages)
func (c *Client) GetEntities() (map[string]any, error) {
	done := make(chan struct{})
	values := make(chan mqtt.Message)

	tok := c.Client.Subscribe(NewTarget("", "+/+/+/+").Topic(), 1, func(c mqtt.Client, m mqtt.Message) {
		if !m.Retained() {
			done <- struct{}{}
		}
		values <- m
	})
	<-tok.Done()
	if err := tok.Error(); err != nil {
		return nil, err
	}

	register := make(map[string]any)
out:
	for {
		select {
		case res := <-values:
			if len(res.Payload()) > 0 {
				payload := make(map[string]any)
				if err := json.Unmarshal(res.Payload(), &payload); err != nil {
					slog.Warn("Could not unmarshal registration message", "err", err)
				} else {
					register[res.Topic()] = payload
				}
			}

		case <-done:
			break out
		case <-time.After(1 * time.Second):
			slog.Debug("Finished waiting for retained messages")
			break out
		}
	}

	return register, nil
}
