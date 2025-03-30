package deribit

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"

	"github.com/fulong98/OptionStream/internal/config"
	"github.com/fulong98/OptionStream/internal/kafka"
	"github.com/gorilla/websocket"
)

type Client struct {
	cfg   config.DeribitConfig
	kafka config.KafkaConfig
	conn  *websocket.Conn
	done  chan struct{}
	mu    sync.RWMutex
}

type OrderbookMessage struct {
	Jsonrpc string `json:"jsonrpc"`
	Method  string `json:"method"`
	Params  struct {
		Channel string `json:"channel"`
		Data    struct {
			Timestamp  int64       `json:"timestamp"`
			Instrument string      `json:"instrument_name"`
			ID         int64       `json:"id"`
			Bids       [][]float64 `json:"bids"`
			Asks       [][]float64 `json:"asks"`
		} `json:"data"`
	} `json:"params"`
}

func NewClient(cfg config.DeribitConfig, kafkaCfg config.KafkaConfig) *Client {
	return &Client{
		cfg:   cfg,
		kafka: kafkaCfg,
		done:  make(chan struct{}),
	}
}

func (c *Client) CollectOrderbook(ctx context.Context, producer *kafka.Producer) error {
	// Connect to Deribit WebSocket
	url := c.cfg.WebSocketURL
	if c.cfg.Testnet {
		url = "wss://test.deribit.com/ws/api/v2"
	}

	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		return fmt.Errorf("failed to connect to Deribit: %w", err)
	}
	c.conn = conn
	defer conn.Close()

	// Subscribe to all instruments
	subscribeMsg := map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  "public/subscribe",
		"params": map[string]interface{}{
			"channels": make([]string, len(c.cfg.Instruments)),
		},
	}

	// Create subscription channels for each instrument
	for i, instrument := range c.cfg.Instruments {
		subscribeMsg["params"].(map[string]interface{})["channels"].([]string)[i] = fmt.Sprintf("book.%s.none.20.100ms", instrument)
	}

	if err := conn.WriteJSON(subscribeMsg); err != nil {
		return fmt.Errorf("failed to subscribe: %w", err)
	}

	log.Printf("Subscribed to %d instruments", len(c.cfg.Instruments))

	// Start reading messages
	go func() {
		defer close(c.done)
		for {
			var msg OrderbookMessage
			if err := conn.ReadJSON(&msg); err != nil {
				log.Printf("Error reading message: %v", err)
				return
			}

			// Convert message to JSON for Kafka
			data, err := json.Marshal(msg)
			if err != nil {
				log.Printf("Error marshaling message: %v", err)
				continue
			}

			// Send to Kafka with instrument name as key
			if err := producer.SendMessage(c.kafka.Topic, msg.Params.Data.Instrument, data); err != nil {
				log.Printf("Error sending to Kafka: %v", err)
			}
		}
	}()

	// Wait for context cancellation
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-c.done:
		return fmt.Errorf("WebSocket connection closed")
	}
}
