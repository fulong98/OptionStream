package deribit

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

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

func (c *Client) connect() (*websocket.Conn, error) {
	url := c.cfg.WebSocketURL
	if c.cfg.Testnet {
		url = "wss://test.deribit.com/ws/api/v2"
	}

	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Deribit: %w", err)
	}

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
		conn.Close()
		return nil, fmt.Errorf("failed to subscribe: %w", err)
	}

	log.Printf("Subscribed to %d instruments", len(c.cfg.Instruments))
	return conn, nil
}
func (c *Client) CollectOrderbook(ctx context.Context, producer *kafka.Producer) error {
	// Main loop for connection and reconnection
	for {
		// Connect
		conn, err := c.connect()
		if err != nil {
			log.Printf("Connection failed: %v, retrying in 5 seconds", err)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(5 * time.Second):
				continue
			}
		}

		c.mu.Lock()
		c.conn = conn
		c.mu.Unlock()

		// Set up connection monitoring
		connClosed := make(chan struct{})

		// Start ping/pong handling
		go func() {
			ticker := time.NewTicker(5 * time.Second)
			defer ticker.Stop()

			// Set ping handler
			conn.SetPingHandler(func(string) error {
				conn.WriteControl(websocket.PongMessage, []byte{}, time.Now().Add(10*time.Second))
				return nil
			})

			// Send regular pings to keep connection alive and detect failures
			for {
				select {
				case <-ticker.C:
					// Set write deadline for ping
					if err := conn.WriteControl(websocket.PingMessage, []byte{},
						time.Now().Add(10*time.Second)); err != nil {
						log.Printf("Ping failed: %v", err)
						close(connClosed)
						return
					}
				case <-connClosed:
					return
				case <-ctx.Done():
					return
				}
			}
		}()

		// Message reader loop
		go func() {
			for {
				// Set read deadline to detect network failures
				conn.SetReadDeadline(time.Now().Add(1 * time.Minute))

				var msg OrderbookMessage
				if err := conn.ReadJSON(&msg); err != nil {
					log.Printf("Read error: %v", err)
					close(connClosed)
					return
				}

				// Reset read deadline after successful read
				conn.SetReadDeadline(time.Time{})

				// Process message
				data, err := json.Marshal(msg)
				if err != nil {
					log.Printf("Error marshaling message: %v", err)
					continue
				}

				if err := producer.SendMessage(c.kafka.Topic, msg.Params.Data.Instrument, data); err != nil {
					log.Printf("Error sending to Kafka: %v", err)
				}
			}
		}()

		// Wait for connection close or context cancellation
		select {
		case <-connClosed:
			log.Println("Connection closed, reconnecting...")
			conn.Close()
			// Add a small delay before reconnecting
			time.Sleep(1 * time.Second)
		case <-ctx.Done():
			conn.WriteMessage(websocket.CloseMessage,
				websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
			conn.Close()
			return ctx.Err()
		}
	}
}

// func (c *Client) CollectOrderbook(ctx context.Context, producer *kafka.Producer) error {
// 	var conn *websocket.Conn
// 	var err error

// 	// Initial connection
// 	conn, err = c.connect()
// 	if err != nil {
// 		return err
// 	}
// 	c.conn = conn
// 	defer conn.Close()

// 	// Create a channel for reconnection
// 	reconnect := make(chan struct{})

// 	// Start reading messages
// 	go func() {
// 		defer close(c.done)
// 		for {
// 			var msg OrderbookMessage
// 			if err := conn.ReadJSON(&msg); err != nil {
// 				if websocket.IsCloseError(err, websocket.CloseNormalClosure) {
// 					log.Println("WebSocket closed normally, attempting to reconnect...")
// 					select {
// 					case reconnect <- struct{}{}:
// 						log.Println("reconnect")
// 					default:
// 						log.Println("default")
// 					}
// 					return
// 				}
// 				log.Printf("Error reading message: %v", err)
// 				return
// 			}

// 			// Convert message to JSON for Kafka
// 			data, err := json.Marshal(msg)
// 			if err != nil {
// 				log.Printf("Error marshaling message: %v", err)
// 				continue
// 			}

// 			// Send to Kafka with instrument name as key
// 			if err := producer.SendMessage(c.kafka.Topic, msg.Params.Data.Instrument, data); err != nil {
// 				log.Printf("Error sending to Kafka: %v", err)
// 			}
// 		}
// 	}()

// 	// Main loop for handling reconnections
// 	for {
// 		select {
// 		case <-ctx.Done():
// 			return ctx.Err()
// 		case <-c.done:
// 			return fmt.Errorf("WebSocket connection closed")
// 		case <-reconnect:
// 			log.Println("Reconnecting to Deribit...")
// 			// Close the old connection
// 			if conn != nil {
// 				conn.Close()
// 			}

// 			// Attempt to reconnect with exponential backoff
// 			var backoff time.Duration = 1 * time.Second
// 			for {
// 				conn, err = c.connect()
// 				if err == nil {
// 					c.conn = conn
// 					break
// 				}
// 				log.Printf("Reconnection attempt failed: %v, retrying in %v", err, backoff)
// 				select {
// 				case <-ctx.Done():
// 					return ctx.Err()
// 				case <-time.After(backoff):
// 					backoff *= 2 // Exponential backoff
// 					log.Println(backoff, "reconnecting")
// 					if backoff > 30*time.Second {
// 						backoff = 30 * time.Second // Cap at 30 seconds
// 					}
// 				}
// 			}
// 		}
// 	}
// }
