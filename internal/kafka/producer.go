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
	writer     *kafkago.Writer
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

func NewProducer(cfg config.KafkaConfig) (*Producer, error) {
	log.Printf("Connecting to Kafka brokers: %v", cfg.Brokers)

	writer := kafkago.NewWriter(kafkago.WriterConfig{
		Brokers:  cfg.Brokers,
		Balancer: &kafkago.LeastBytes{},
		Dialer: &kafkago.Dialer{
			Timeout:   10 * time.Second,
			DualStack: true,
			KeepAlive: 60 * time.Second,
		},
		WriteTimeout:     10 * time.Second,
		ReadTimeout:      10 * time.Second,
		Async:            false,
		BatchSize:        cfg.BatchSize,
		BatchTimeout:     time.Duration(cfg.BatchTimeoutMs) * time.Millisecond,
		CompressionCodec: kafkago.Snappy.Codec(),
	})

	// Test the connection
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	log.Printf("Testing Kafka connection...")
	if err := writer.WriteMessages(ctx, kafkago.Message{
		Topic: cfg.Topic,
		Value: []byte("test connection"),
	}); err != nil {
		return nil, fmt.Errorf("failed to connect to Kafka: %w", err)
	}

	log.Printf("Successfully connected to Kafka")

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

	// Add message IDs for idempotency
	for i := range batch {
		batch[i].Key = []byte(fmt.Sprintf("%d", time.Now().UnixNano()+int64(i)))
	}

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

	ticker := time.NewTicker(time.Minute)
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

func (p *Producer) SendMessage(topic string, message []byte) error {
	select {
	case p.messageCh <- kafkago.Message{
		Topic: topic,
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
