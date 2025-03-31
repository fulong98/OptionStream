package consumer

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"sync"
	"time"

	"github.com/fulong98/OptionStream/internal/config"
	"github.com/fulong98/OptionStream/internal/deribit"
	"github.com/segmentio/kafka-go"
)

// OrderbookState represents the current state of the orderbook for an instrument
type OrderbookState struct {
	Instrument string              `json:"instrument"`
	Timestamp  int64               `json:"timestamp"`
	Bids       map[float64]float64 `json:"-"`
	Asks       map[float64]float64 `json:"-"`
}

// Custom JSON marshaling to handle float64 map keys
func (ob *OrderbookState) MarshalJSON() ([]byte, error) {
	// Create a temporary struct with string versions of the maps
	type OrderbookJSON struct {
		Instrument string             `json:"instrument"`
		Timestamp  int64              `json:"timestamp"`
		Bids       map[string]float64 `json:"bids"` // String keys work with JSON
		Asks       map[string]float64 `json:"asks"` // String keys work with JSON
	}

	jsonOb := OrderbookJSON{
		Instrument: ob.Instrument,
		Timestamp:  ob.Timestamp,
		Bids:       make(map[string]float64, len(ob.Bids)),
		Asks:       make(map[string]float64, len(ob.Asks)),
	}

	// Convert float64 keys to string keys
	for price, quantity := range ob.Bids {
		jsonOb.Bids[fmt.Sprintf("%.8f", price)] = quantity
	}

	for price, quantity := range ob.Asks {
		jsonOb.Asks[fmt.Sprintf("%.8f", price)] = quantity
	}

	return json.Marshal(jsonOb)
}

// UnmarshalJSON implements custom JSON unmarshaling
func (ob *OrderbookState) UnmarshalJSON(data []byte) error {
	// Create a temporary struct with string versions of the maps
	type OrderbookJSON struct {
		Instrument string             `json:"instrument"`
		Timestamp  int64              `json:"timestamp"`
		Bids       map[string]float64 `json:"bids"`
		Asks       map[string]float64 `json:"asks"`
	}

	var jsonOb OrderbookJSON
	if err := json.Unmarshal(data, &jsonOb); err != nil {
		return err
	}

	// Initialize the OrderbookState
	ob.Instrument = jsonOb.Instrument
	ob.Timestamp = jsonOb.Timestamp
	ob.Bids = make(map[float64]float64, len(jsonOb.Bids))
	ob.Asks = make(map[float64]float64, len(jsonOb.Asks))

	// Convert string keys back to float64 keys
	for priceStr, quantity := range jsonOb.Bids {
		price, err := strconv.ParseFloat(priceStr, 64)
		if err != nil {
			return err
		}
		ob.Bids[price] = quantity
	}

	for priceStr, quantity := range jsonOb.Asks {
		price, err := strconv.ParseFloat(priceStr, 64)
		if err != nil {
			return err
		}
		ob.Asks[price] = quantity
	}

	return nil
}

// Consumer interface defines the methods needed for any orderbook data consumer
type Consumer interface {
	// Start initializes and starts the consumer
	Start(ctx context.Context) error

	// Subscribe registers a new subscriber for orderbook updates
	Subscribe() chan *OrderbookState

	// Unsubscribe removes a subscriber
	Unsubscribe(ch chan *OrderbookState)

	// GetOrderbook returns the current state of an orderbook
	GetOrderbook(instrument string) (*OrderbookState, bool)

	// GetInstruments returns a list of all available instruments
	GetInstruments() []string

	// IsRunning checks if the consumer is currently running
	IsRunning() bool
}

// KafkaConsumerInterface extends the Consumer interface with Kafka-specific methods
type KafkaConsumerInterface interface {
	Consumer

	// ConsumeInstrument starts consuming data for a specific instrument
	ConsumeInstrument(instrument string) error

	// Close shuts down the consumer and all its readers
	Close() error
}

// MockConsumerInterface extends the Consumer interface with mock-specific methods
type MockConsumerInterface interface {
	Consumer
}

// KafkaConsumer consumes orderbook data from Kafka for specific instruments
type KafkaConsumer struct {
	cfg          config.KafkaConfig
	orderbooks   map[string]*OrderbookState // instrument -> orderbook state
	readers      map[string]*kafka.Reader   // instrument -> kafka reader
	mu           sync.RWMutex
	subscribers  map[chan *OrderbookState]bool
	subMu        sync.RWMutex
	activeMu     sync.Mutex
	activeReader string
	balancer     kafka.Balancer
	ctx          context.Context
	cancel       context.CancelFunc
	waitGroup    sync.WaitGroup
}

// NewKafkaConsumer creates a new Kafka consumer
func NewKafkaConsumer(cfg config.KafkaConfig) *KafkaConsumer {
	ctx, cancel := context.WithCancel(context.Background())
	return &KafkaConsumer{
		cfg:         cfg,
		orderbooks:  make(map[string]*OrderbookState),
		readers:     make(map[string]*kafka.Reader),
		subscribers: make(map[chan *OrderbookState]bool),
		balancer:    &kafka.CRC32Balancer{},
		ctx:         ctx,
		cancel:      cancel,
	}
}

// Start initializes the consumer but doesn't start reading yet
func (k *KafkaConsumer) Start(ctx context.Context) error {
	log.Println("Kafka consumer initialized and ready")
	return nil
}

// GetPartitionForInstrument determines which partition an instrument's data will be in
func (k *KafkaConsumer) GetPartitionForInstrument(instrument string) int {
	// Create a message with the instrument as key
	msg := kafka.Message{
		Key: []byte(instrument),
	}

	// Create a list of all partitions
	partitions := make([]int, 64) // Assuming 64 partitions
	for i := 0; i < 64; i++ {
		partitions[i] = i
	}

	// Use the balancer to determine which partition this instrument belongs to
	return k.balancer.Balance(msg, partitions...)
}

// ConsumeInstrument starts consuming data for a specific instrument
func (k *KafkaConsumer) ConsumeInstrument(instrument string) error {
	k.activeMu.Lock()
	defer k.activeMu.Unlock()

	// If we're already consuming this instrument, do nothing
	if k.activeReader == instrument {
		return nil
	}

	// Stop any existing reader
	if k.activeReader != "" {
		k.stopReader(k.activeReader)
	}

	// Set the new active reader
	k.activeReader = instrument

	// Get the partition for this instrument
	partition := k.GetPartitionForInstrument(instrument)
	log.Printf("Instrument %s will be read from partition %d", instrument, partition)

	// Create a reader for this instrument
	reader := kafka.NewReader(kafka.ReaderConfig{
		Brokers:   k.cfg.Brokers,
		Topic:     k.cfg.Topic,
		Partition: partition,
		MaxBytes:  10e6, // 10MB
		MinBytes:  10e3, // 10KB
	})
	reader.SetOffset(0)

	// Store the reader
	k.mu.Lock()
	k.readers[instrument] = reader
	k.mu.Unlock()

	// Set offset to beginning to get all historical data
	if err := reader.SetOffset(0); err != nil {
		return err
	}

	// Start reading in a goroutine
	k.waitGroup.Add(1)
	go func() {
		defer k.waitGroup.Done()
		k.readMessages(instrument, reader)
	}()

	return nil
}

// readMessages continuously reads messages for a specific instrument
func (k *KafkaConsumer) readMessages(instrument string, reader *kafka.Reader) {
	// log.Printf("Reading messages for instrument: %s", instrument)
	defer func() {
		if err := reader.Close(); err != nil {
			log.Printf("Error closing reader for %s: %v", instrument, err)
		}
	}()

	// Create a timeout context for each read operation
	for {
		select {
		case <-k.ctx.Done():
			return
		default:
			// readCtx, cancel := context.WithTimeout(k.ctx, 10*time.Second)
			m, err := reader.ReadMessage(context.Background())
			// log.Printf("Read message for %s: %v", instrument, m)
			// cancel()

			if err != nil {
				log.Printf("Error reading message for %s: %v", instrument, err)
				// Check if context was canceled
				if k.ctx.Err() != nil {
					return
				}

				log.Printf("Error reading message for %s: %v", instrument, err)
				time.Sleep(100 * time.Millisecond)
				continue
			}

			// We only want messages for our specific instrument
			if string(m.Key) != instrument {
				// log.Printf("Skipping message for instrument: %s", string(m.Key))
				continue
			}

			// Parse the message
			var msg deribit.OrderbookMessage
			if err := json.Unmarshal(m.Value, &msg); err != nil {
				log.Printf("Error unmarshaling message: %v", err)
				continue
			}

			log.Printf("Parsed Orderbook: instrument=%s | bids=%v | asks=%v | ts=%d",
				instrument, msg.Params.Data.Bids, msg.Params.Data.Asks, msg.Params.Data.Timestamp)

			// Only process subscription messages with orderbook data
			if msg.Method != "subscription" {
				continue
			}

			// Update the orderbook
			log.Printf("Updating orderbook for instrument: %s", instrument)
			k.updateOrderbook(instrument, msg.Params.Data.Timestamp, msg.Params.Data.Bids, msg.Params.Data.Asks)
		}
	}
}

// stopReader stops and removes a reader for an instrument
func (k *KafkaConsumer) stopReader(instrument string) {
	k.mu.Lock()
	defer k.mu.Unlock()

	reader, exists := k.readers[instrument]
	if !exists {
		return
	}

	if err := reader.Close(); err != nil {
		log.Printf("Error closing reader for %s: %v", instrument, err)
	}

	delete(k.readers, instrument)
}

func (k *KafkaConsumer) updateOrderbook(instrument string, timestamp int64, bids, asks [][]float64) {
	k.mu.Lock()

	// Get or create orderbook state
	ob, exists := k.orderbooks[instrument]
	if !exists {
		ob = &OrderbookState{
			Instrument: instrument,
			Bids:       make(map[float64]float64),
			Asks:       make(map[float64]float64),
		}
		k.orderbooks[instrument] = ob
		log.Printf("New instrument detected: %s", instrument)
	}

	// Update timestamp
	ob.Timestamp = timestamp

	// For full refreshes, clear existing data and replace with new data
	// Clear existing orderbook state
	ob.Bids = make(map[float64]float64)
	ob.Asks = make(map[float64]float64)

	// Update bids with new data
	for _, bid := range bids {
		if len(bid) >= 2 {
			price, quantity := bid[0], bid[1]
			if quantity > 0 {
				ob.Bids[price] = quantity
			}
		}
	}

	// Update asks with new data
	for _, ask := range asks {
		if len(ask) >= 2 {
			price, quantity := ask[0], ask[1]
			if quantity > 0 {
				ob.Asks[price] = quantity
			}
		}
	}

	// Log orderbook state (especially useful for debugging)
	if len(ob.Bids) > 0 || len(ob.Asks) > 0 {
		log.Printf("Updated orderbook for %s: %d bids, %d asks",
			instrument, len(ob.Bids), len(ob.Asks))
	} else {
		log.Printf("Warning: Empty orderbook for %s after update", instrument)
	}

	// Make a copy for subscribers
	snapshot := &OrderbookState{
		Instrument: ob.Instrument,
		Timestamp:  ob.Timestamp,
		Bids:       make(map[float64]float64, len(ob.Bids)),
		Asks:       make(map[float64]float64, len(ob.Asks)),
	}

	for k, v := range ob.Bids {
		snapshot.Bids[k] = v
	}
	for k, v := range ob.Asks {
		snapshot.Asks[k] = v
	}

	k.mu.Unlock()

	// Notify subscribers
	k.notifySubscribers(snapshot)
}

// notifySubscribers sends orderbook updates to all subscribers
func (k *KafkaConsumer) notifySubscribers(ob *OrderbookState) {
	k.subMu.RLock()
	defer k.subMu.RUnlock()

	for ch := range k.subscribers {
		select {
		case ch <- ob:
			// Successfully sent
		default:
			// Channel is full, skip this update for this subscriber
		}
	}
}

// Subscribe registers a new subscriber for orderbook updates
func (k *KafkaConsumer) Subscribe() chan *OrderbookState {
	ch := make(chan *OrderbookState, 100)

	k.subMu.Lock()
	k.subscribers[ch] = true
	k.subMu.Unlock()

	return ch
}

// Unsubscribe removes a subscriber
func (k *KafkaConsumer) Unsubscribe(ch chan *OrderbookState) {
	k.subMu.Lock()
	delete(k.subscribers, ch)
	k.subMu.Unlock()

	close(ch)
}

// GetOrderbook returns the current state of an orderbook
func (k *KafkaConsumer) GetOrderbook(instrument string) (*OrderbookState, bool) {
	k.mu.RLock()
	defer k.mu.RUnlock()

	ob, exists := k.orderbooks[instrument]
	if !exists {
		return nil, false
	}

	// Make a copy to avoid race conditions
	snapshot := &OrderbookState{
		Instrument: ob.Instrument,
		Timestamp:  ob.Timestamp,
		Bids:       make(map[float64]float64, len(ob.Bids)),
		Asks:       make(map[float64]float64, len(ob.Asks)),
	}

	for k, v := range ob.Bids {
		snapshot.Bids[k] = v
	}
	for k, v := range ob.Asks {
		snapshot.Asks[k] = v
	}

	return snapshot, true
}

// GetInstruments returns a list of all instruments (from config)
func (k *KafkaConsumer) GetInstruments() []string {
	k.mu.RLock()
	defer k.mu.RUnlock()

	instruments := make([]string, 0, len(k.orderbooks))
	for instrument := range k.orderbooks {
		instruments = append(instruments, instrument)
	}

	return instruments
}

// Close shuts down the consumer
func (k *KafkaConsumer) Close() error {
	// Signal all goroutines to stop
	k.cancel()

	// Stop all readers
	k.mu.Lock()
	for instrument, reader := range k.readers {
		if err := reader.Close(); err != nil {
			log.Printf("Error closing reader for %s: %v", instrument, err)
		}
	}
	k.readers = make(map[string]*kafka.Reader)
	k.mu.Unlock()

	k.waitGroup.Wait()

	return nil
}

// IsRunning always returns true for compatibility with the consumer interface
func (k *KafkaConsumer) IsRunning() bool {
	return true
}
