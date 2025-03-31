package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/fulong98/OptionStream/internal/config"
	"github.com/fulong98/OptionStream/internal/consumer"
	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true // Allow all origins for testing
	},
}

type WebSocketClient struct {
	conn *websocket.Conn
	send chan []byte
}

func main() {

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	kafkaConsumer := consumer.NewKafkaConsumer(cfg.Kafka)

	if err := kafkaConsumer.Start(ctx); err != nil {
		log.Fatalf("Failed to start Kafka consumer: %v", err)
	}
	defer kafkaConsumer.Close()

	// Create a router
	r := mux.NewRouter()

	// REST API - Get all instruments (from config)
	r.HandleFunc("/api/instruments", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(cfg.Deribit.Instruments)
	}).Methods("GET")

	// REST API - Get orderbook for a specific instrument
	r.HandleFunc("/api/orderbook/{instrument}", func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		instrument := vars["instrument"]

		// Try to get current orderbook if available
		orderbook, exists := kafkaConsumer.GetOrderbook(instrument)
		if !exists {
			// If not available, start consuming for this instrument
			if err := kafkaConsumer.ConsumeInstrument(instrument); err != nil {
				log.Printf("Error starting consumption for %s: %v", instrument, err)
				http.Error(w, "Failed to start data collection", http.StatusInternalServerError)
				return
			}

			// Create an empty orderbook for now
			orderbook = &consumer.OrderbookState{
				Instrument: instrument,
				Timestamp:  time.Now().UnixNano() / int64(time.Millisecond),
				Bids:       make(map[float64]float64),
				Asks:       make(map[float64]float64),
			}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(orderbook)
	}).Methods("GET")

	// WebSocket endpoint for real-time orderbook updates
	r.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		handleWebSocketConnection(w, r, kafkaConsumer)
	})

	// Start the HTTP server
	srv := &http.Server{
		Addr:         ":8081",
		Handler:      r,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	// Run the server in a goroutine
	go func() {
		log.Printf("Starting server on :8081")
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("HTTP server error: %v", err)
		}
	}()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	cancel()

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Fatalf("Server shutdown error: %v", err)
	}
}
func handleWebSocketConnection(w http.ResponseWriter, r *http.Request, consumer *consumer.KafkaConsumer) {
	// Upgrade HTTP connection to WebSocket
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("Failed to upgrade connection: %v", err)
		return
	}
	defer conn.Close()

	client := &WebSocketClient{
		conn: conn,
		send: make(chan []byte, 256),
	}

	currentInstrument := r.URL.Query().Get("instrument")
	if currentInstrument == "" {
		log.Println("WebSocket connection without instrument parameter")
		return
	}

	log.Printf("WebSocket client connected, requesting instrument: %s", currentInstrument)

	if err := consumer.ConsumeInstrument(currentInstrument); err != nil {
		log.Printf("Error starting consumption for %s: %v", currentInstrument, err)
		return
	}

	orderbookChan := consumer.Subscribe()
	defer consumer.Unsubscribe(orderbookChan)

	// Send initial orderbook state if available
	if ob, exists := consumer.GetOrderbook(currentInstrument); exists {
		data, err := json.Marshal(ob)
		if err == nil {
			client.send <- data
		}
	}

	// Start the write pump in a goroutine
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		writeMessages(client)
	}()

	// Read pump - handle incoming messages (filter by instrument)
	// Using a separate context with cancellation for clean shutdown
	ctx, cancel := context.WithCancel(context.Background())
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-ctx.Done():
				return
			case ob, ok := <-orderbookChan:
				if !ok {
					// Channel closed
					return
				}

				// Only forward updates for the current instrument
				if ob.Instrument == currentInstrument {
					data, err := json.Marshal(ob)
					if err != nil {
						log.Printf("Error marshaling orderbook: %v", err)
						continue
					}

					select {
					case client.send <- data:
						// Message sent
					default:
						// Channel full, discard message
						log.Printf("Client send buffer full, discarding message")
					}
				}
			}
		}
	}()

	// Handle client commands
	for {
		_, message, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("WebSocket error: %v", err)
			} else {
				log.Printf("WebSocket closed: %v", err)
			}
			break
		}

		// Parse the client message
		var cmd struct {
			Action     string `json:"action"`
			Instrument string `json:"instrument"`
		}

		if err := json.Unmarshal(message, &cmd); err != nil {
			log.Printf("Error parsing client message: %v", err)
			continue
		}

		log.Printf("Received command: %s for instrument: %s", cmd.Action, cmd.Instrument)

		if cmd.Action == "subscribe" && cmd.Instrument != "" && cmd.Instrument != currentInstrument {
			// Update the current instrument
			log.Printf("Client switching from %s to %s", currentInstrument, cmd.Instrument)
			currentInstrument = cmd.Instrument

			// Start consuming data for this instrument
			if err := consumer.ConsumeInstrument(currentInstrument); err != nil {
				log.Printf("Error starting consumption for %s: %v", currentInstrument, err)
				// Send an error message to the client
				errorMsg, _ := json.Marshal(map[string]string{
					"error": fmt.Sprintf("Failed to subscribe to %s", currentInstrument),
				})
				client.send <- errorMsg
				continue
			}

			// Send current orderbook state if available
			if ob, exists := consumer.GetOrderbook(currentInstrument); exists {
				log.Printf("Sending initial orderbook for %s", currentInstrument)
				data, err := json.Marshal(ob)
				if err == nil {
					client.send <- data
				}
			} else {
				log.Printf("No existing orderbook found for %s, waiting for updates", currentInstrument)
				// Send a message indicating we're waiting for data
				statusMsg, _ := json.Marshal(map[string]string{
					"status": fmt.Sprintf("Subscribed to %s, waiting for data", currentInstrument),
				})
				client.send <- statusMsg
			}
		}
	}

	// Signal the orderbook pump to exit
	cancel()

	// Close the send channel to signal the write pump to exit
	close(client.send)

	// Wait for goroutines to finish
	wg.Wait()
	log.Printf("WebSocket connection handler completed for %s", currentInstrument)
}

func writeMessages(client *WebSocketClient) {
	ticker := time.NewTicker(time.Second * 30)
	defer ticker.Stop()

	for {
		select {
		case message, ok := <-client.send:
			if !ok {
				// The channel is closed, exit
				client.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			w, err := client.conn.NextWriter(websocket.TextMessage)
			if err != nil {
				return
			}
			w.Write(message)

			// Add queued messages
			n := len(client.send)
			for i := 0; i < n; i++ {
				w.Write([]byte{'\n'})
				w.Write(<-client.send)
			}

			if err := w.Close(); err != nil {
				return
			}
		case <-ticker.C:
			// Send ping to keep connection alive
			if err := client.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}
