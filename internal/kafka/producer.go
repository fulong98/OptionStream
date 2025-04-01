package kafka

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/fulong98/OptionStream/internal/config"
	kafkago "github.com/segmentio/kafka-go"
)

type Producer struct {
	writer     Writer
	messageCh  chan kafkago.Message
	closeCh    chan struct{}
	wg         sync.WaitGroup
	batchSize  int
	batchDelay time.Duration
	stats      struct {
		sync.RWMutex
		messagesReceived int64
		messagesSent     int64
		errors           int64
		lastError        error
		lastErrorTime    time.Time
		lastFlushTime    time.Time
		lastBatchSize    int
		retryCount       int64
	}
}

type Writer interface {
	WriteMessages(ctx context.Context, msgs ...kafkago.Message) error
	Close() error
	Stats() kafkago.WriterStats
}

func NewProducer(cfg config.KafkaConfig) (*Producer, error) {
	log.Printf("Connecting to Kafka brokers: %v", cfg.Brokers)

	dialer := &kafkago.Dialer{
		Timeout:   10 * time.Second,
		DualStack: true,
		KeepAlive: 60 * time.Second,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	log.Printf("Testing Kafka connection...")
	conn, err := dialer.DialContext(ctx, "tcp", cfg.Brokers[0])
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Kafka: %w", err)
	}

	// Verify topic exists
	partitions, err := conn.ReadPartitions(cfg.Topic)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("failed to read topic metadata: %w", err)
	}

	log.Printf("Successfully connected to Kafka: topic %s has %d partitions",
		cfg.Topic, len(partitions))
	conn.Close()

	writer := kafkago.NewWriter(kafkago.WriterConfig{
		Brokers: cfg.Brokers,
		// Topic:            cfg.Topic, // Set default topic
		Balancer:         &kafkago.CRC32Balancer{},
		Dialer:           dialer,
		WriteTimeout:     10 * time.Second,
		ReadTimeout:      10 * time.Second,
		Async:            false,
		BatchSize:        cfg.BatchSize,
		BatchTimeout:     time.Duration(cfg.BatchTimeoutMs) * time.Millisecond,
		CompressionCodec: kafkago.Snappy.Codec(),
	})

	producer := &Producer{
		writer:     writer,
		messageCh:  make(chan kafkago.Message, cfg.BatchSize*2),
		closeCh:    make(chan struct{}),
		batchSize:  cfg.BatchSize,
		batchDelay: time.Duration(cfg.BatchTimeoutMs) * time.Millisecond,
	}

	// Start the batch processor and stats monitor
	producer.wg.Add(2)
	go producer.processBatches()
	go producer.monitorStats()

	return producer, nil
}

func NewProducerWithWriter(cfg config.KafkaConfig, writer Writer) (*Producer, error) {
	producer := &Producer{
		writer:     writer,
		messageCh:  make(chan kafkago.Message, cfg.BatchSize*2),
		closeCh:    make(chan struct{}),
		batchSize:  cfg.BatchSize,
		batchDelay: time.Duration(cfg.BatchTimeoutMs) * time.Millisecond,
	}

	// Start the batch processor and stats monitor
	producer.wg.Add(2)
	go producer.processBatches()
	go producer.monitorStats()

	return producer, nil
}

func (p *Producer) processBatches() {
	defer p.wg.Done()

	batch := make([]kafkago.Message, 0, p.batchSize)
	ticker := time.NewTicker(p.batchDelay)
	defer ticker.Stop()

	for {
		select {
		case msg := <-p.messageCh:
			batch = append(batch, msg)
			p.incrementReceived()
			if len(batch) >= p.batchSize {
				p.flushBatch(batch)
				batch = batch[:0]
			}
		case <-ticker.C:
			if len(batch) > 0 {
				p.flushBatch(batch)
				batch = batch[:0]
			}
		case <-p.closeCh:
			if len(batch) > 0 {
				p.flushBatch(batch)
			}
			return
		}
	}
}

func (p *Producer) flushBatch(batch []kafkago.Message) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := p.writer.WriteMessages(ctx, batch...); err != nil {
		p.recordError(err)
		log.Printf("Error flushing batch: %v", err)
		return
	}

	p.recordSuccess(len(batch))
	p.stats.Lock()
	p.stats.lastFlushTime = time.Now()
	p.stats.lastBatchSize = len(batch)
	p.stats.Unlock()
}

func (p *Producer) monitorStats() {
	defer p.wg.Done()

	ticker := time.NewTicker(time.Second * 5)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			p.stats.RLock()
			log.Printf("Kafka Stats - Received: %d, Sent: %d, Errors: %d, Retries: %d, Last Error: %v, Last Batch Size: %d",
				p.stats.messagesReceived,
				p.stats.messagesSent,
				p.stats.errors,
				p.stats.retryCount,
				p.stats.lastError,
				p.stats.lastBatchSize)
			p.stats.RUnlock()
		case <-p.closeCh:
			return
		}
	}
}

func (p *Producer) incrementReceived() {
	p.stats.Lock()
	p.stats.messagesReceived++
	p.stats.Unlock()
}

func (p *Producer) recordSuccess(count int) {
	p.stats.Lock()
	p.stats.messagesSent += int64(count)
	p.stats.Unlock()
}

func (p *Producer) recordError(err error) {
	p.stats.Lock()
	p.stats.errors++
	p.stats.lastError = err
	p.stats.lastErrorTime = time.Now()
	if kafkaErr, ok := err.(kafkago.Error); ok && kafkaErr.Temporary() {
		p.stats.retryCount++
	}
	p.stats.Unlock()
}

func (p *Producer) SendMessage(topic string, instrumentName string, message []byte) error {
	if len(message) == 0 {
		return fmt.Errorf("empty message, skipping")
	}

	// Check if instrument name is empty
	if instrumentName == "" {
		return fmt.Errorf("empty instrument name, skipping")
	}

	select {
	case p.messageCh <- kafkago.Message{
		Topic: topic,
		Key:   []byte(instrumentName),
		Value: message,
	}:
		return nil
	case <-p.closeCh:
		return fmt.Errorf("producer is closed")
	default:
		return fmt.Errorf("message channel is full, dropping message")
	}
}

func (p *Producer) Close() error {
	close(p.closeCh)
	p.wg.Wait()
	return p.writer.Close()
}

func (p *Producer) GetMessagesReceived() int64 {
	p.stats.RLock()
	defer p.stats.RUnlock()
	return p.stats.messagesReceived
}

func (p *Producer) GetMessagesSent() int64 {
	p.stats.RLock()
	defer p.stats.RUnlock()
	return p.stats.messagesSent
}

func (p *Producer) GetErrors() int64 {
	p.stats.RLock()
	defer p.stats.RUnlock()
	return p.stats.errors
}

func (p *Producer) GetRetryCount() int64 {
	p.stats.RLock()
	defer p.stats.RUnlock()
	return p.stats.retryCount
}
