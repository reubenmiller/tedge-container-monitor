package tedge

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/reubenmiller/go-c8y/pkg/c8y"
)

var StatusUp = "up"
var StatusDown = "down"
var StatusUnknown = "unknown"

func PayloadHealthStatusDown() string {
	return fmt.Sprintf(`{"status":"%s"}`, StatusDown)
}

func PayloadHealthStatus(payload map[string]any, status string) ([]byte, error) {
	payload["status"] = status
	payload["time"] = time.Now().Unix()
	b, err := json.Marshal(payload)
	return b, err
}

func PayloadRegistration(payload map[string]any, name string, entityType string, parent string) ([]byte, error) {
	payload["@type"] = entityType
	payload["name"] = name
	if parent != "" {
		payload["@parent"] = parent
	}
	b, err := json.Marshal(payload)
	return b, err
}

type Client struct {
	Parent           Target
	ServiceName      string
	Client           mqtt.Client
	Target           Target
	CumulocityClient *c8y.Client

	Entities map[string]any
	mutex    sync.RWMutex
}

type ClientConfig struct {
	MqttHost string
	MqttPort uint16
	C8yHost  string
	C8yPort  uint16

	OnConnection func()
}

func NewClientConfig() *ClientConfig {
	return &ClientConfig{
		MqttHost: "127.0.0.1",
		MqttPort: 1883,
		C8yHost:  "127.0.0.1",
		C8yPort:  8001,
	}
}

func NewClient(parent Target, target Target, serviceName string, config *ClientConfig) *Client {
	opts := mqtt.NewClientOptions()
	opts.AddBroker(fmt.Sprintf("tcp://%s:%d", config.MqttHost, config.MqttPort))
	opts.SetClientID(serviceName)
	opts.SetClientID(fmt.Sprintf("%s#%s", serviceName, target.Topic()))
	opts.SetCleanSession(true)
	// opts.SetOrderMatters(true)
	opts.SetWill(GetHealthTopic(target), PayloadHealthStatusDown(), 1, true)
	opts.SetAutoReconnect(true)
	opts.SetAutoAckDisabled(false)
	opts.SetResumeSubs(false)
	opts.SetKeepAlive(60 * time.Second)

	opts.SetOnConnectHandler(func(c mqtt.Client) {
		slog.Info("MQTT Client is connected")
	})
	opts.SetConnectionLostHandler(func(c mqtt.Client, err error) {
		slog.Info("MQTT Client is disconnected.", "err", err)
	})

	opts.SetOnConnectHandler(func(c mqtt.Client) {
		if config.OnConnection != nil {
			config.OnConnection()
		}

		// TODO: Find a cleaner way to prevent a race condition between
		// the registration message and the health/status
		// Maybe the connect() function logic should be moved here instead
		time.Sleep(500 * time.Millisecond)
		payload, err := PayloadHealthStatus(map[string]any{}, StatusUp)
		if err != nil {
			return
		}
		topic := GetHealthTopic(target)
		tok := c.Publish(topic, 1, true, payload)
		<-tok.Done()
		if err := tok.Error(); err != nil {
			slog.Warn("Failed to publish health message.", "err", err)
			return
		}
		slog.Info("Published health message.", "topic", topic, "payload", payload)
	})

	client := mqtt.NewClient(opts)

	// TODO: Read port and host from settings
	// TODO: Support local certificate based auth
	c8yURL := fmt.Sprintf("http://%s:%d/c8y", config.C8yHost, config.C8yPort)
	c8yclient := c8y.NewClient(nil, c8yURL, "", "", "", true)

	slog.Info("MQTT Client options.", "clientID", opts.ClientID)

	c := &Client{
		ServiceName:      serviceName,
		Client:           client,
		Parent:           parent,
		Target:           target,
		CumulocityClient: c8yclient,
		Entities:         make(map[string]any),
	}

	registrationTopics := GetTopic(*target.Service("+"))
	slog.Info("Subscribing to registration topics.", "topic", registrationTopics)
	c.Client.AddRoute(GetTopic(*target.Service("+")), func(mqttc mqtt.Client, m mqtt.Message) {
		go c.handleRegistrationMessage(mqttc, m)
	})
	return c
}

func (c *Client) handleRegistrationMessage(_ mqtt.Client, m mqtt.Message) {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	if len(m.Payload()) > 0 {
		payload := make(map[string]any)
		if err := json.Unmarshal(m.Payload(), &payload); err != nil {
			slog.Warn("Could not unmarshal registration message", "err", err)
		} else {
			c.Entities[m.Topic()] = payload
		}
	} else {
		slog.Info("Removing entity from store.", "topic", m.Topic())
		delete(c.Entities, m.Topic())
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

	payload, err := PayloadRegistration(map[string]any{}, c.ServiceName, "service", c.Parent.TopicID)
	if err != nil {
		return err
	}
	tok = c.Client.Publish(GetTopicRegistration(c.Target), 1, true, payload)
	<-tok.Done()
	if err := tok.Error(); err != nil {
		return err
	}
	slog.Info("Registered service", "topic", GetTopicRegistration(c.Target))

	// TODO: Let the caller decide which topics to subscribe to
	subscriptions := make(map[string]byte)
	subscriptions[c.Target.RootPrefix+"/+/+/+/+"] = 1
	subscriptions[GetTopic(*c.Target.Service("+"), "cmd", "health", "check")] = 1
	slog.Info("Subscribing to topics.", "topics", subscriptions)
	tok = c.Client.SubscribeMultiple(subscriptions, nil)
	tok.Wait()
	return tok.Error()
}

// Delete a Cumulocity Managed object by External ID
func (c *Client) DeleteCumulocityManagedObject(target Target) (bool, error) {
	slog.Info("Deleting service by external ID.", "name", target.ExternalID())
	extID, resp, err := c.CumulocityClient.Identity.GetExternalID(context.Background(), "c8y_Serial", target.ExternalID())

	if err != nil {
		if resp != nil && resp.StatusCode() == http.StatusNotFound {
			return false, nil
		}
		return false, err
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

// func (c *Client) Register(topics []string, qos byte, handler MessageHandler) error {
// 	handlerWrapper := func(c mqtt.Client, m mqtt.Message) {
// 		payloadLen := len(m.Payload())
// 		slog.Info("Received message.", "topic", m.Topic(), "payload_len", payloadLen)

// 		if payloadLen == 0 {
// 			slog.Info("Ignoring empty message", "topic", m.Topic())
// 			return
// 		}
// 		handler(m.Topic(), string(m.Payload()))
// 	}

// 	for _, topic := range topics {
// 		if _, exists := s.Subscriptions[topic]; exists {
// 			slog.Warn("Duplicate topic detected. The new handler will replace the previous one.", "topic", topic)
// 		}
// 		s.Subscriptions[topic] = qos
// 		slog.Info("Adding mqtt route.", "topic", topic)
// 		s.Client.AddRoute(topic, handlerWrapper)
// 	}
// 	return nil
// }

// Get the thin-edge.io entities that have already been registered (as retained messages)
func (c *Client) GetEntities() (map[string]any, error) {
	c.mutex.RLock()
	defer c.mutex.RUnlock()
	return c.Entities, nil
}
