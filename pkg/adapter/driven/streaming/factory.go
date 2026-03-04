package streaming

import (
	"fmt"

	"github.com/digitally-rendered/stellar-drive/pkg/core/port"
)

// New creates an EventStreamer for the named adapter. Supported adapter values
// are "kafka", "nats", and "redis". The config map supplies adapter-specific
// connection parameters:
//
//   - kafka:  "brokers" []string  (default: ["localhost:9092"])
//   - nats:   "url"     string    (default: "nats://localhost:4222")
//   - redis:  "addr"    string    (default: "localhost:6379")
//
// An unknown adapter name returns an error that lists the supported names.
func New(adapter string, config map[string]any) (port.EventStreamer, error) {
	switch adapter {
	case "kafka":
		brokers := []string{"localhost:9092"}
		if v, ok := config["brokers"]; ok {
			switch b := v.(type) {
			case []string:
				brokers = b
			case []any:
				brokers = make([]string, 0, len(b))
				for _, item := range b {
					if s, ok := item.(string); ok {
						brokers = append(brokers, s)
					}
				}
			}
		}
		return NewKafkaStreamer(brokers), nil

	case "nats":
		url := "nats://localhost:4222"
		if v, ok := config["url"]; ok {
			if s, ok := v.(string); ok && s != "" {
				url = s
			}
		}
		return NewNATSStreamer(url), nil

	case "redis":
		addr := "localhost:6379"
		if v, ok := config["addr"]; ok {
			if s, ok := v.(string); ok && s != "" {
				addr = s
			}
		}
		return NewRedisStreamer(addr), nil

	default:
		return nil, fmt.Errorf("streaming: unknown adapter %q (supported: kafka, nats, redis)", adapter)
	}
}
