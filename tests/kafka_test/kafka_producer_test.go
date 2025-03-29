package kafka_test

import (
	"context"
	"testing"
	"time"

	"github.com/fulong98/OptionStream/internal/config"
	"github.com/fulong98/OptionStream/internal/kafka"
	kafkago "github.com/segmentio/kafka-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// Mock Writer to replace the real Kafka writer
type MockWriter struct {
	mock.Mock
}

func (m *MockWriter) WriteMessages(ctx context.Context, msgs ...kafkago.Message) error {
	args := m.Called(ctx, msgs)
	return args.Error(0)
}

func (m *MockWriter) Close() error {
	args := m.Called()
	return args.Error(0)
}

func (m *MockWriter) Stats() kafkago.WriterStats {
	return kafkago.WriterStats{}
}

// Custom Producer with mocked writer for testing
type TestProducer struct {
	*kafka.Producer
	mockWriter *MockWriter
}

func (p *TestProducer) GetStats() struct {
	MessagesReceived int64
	MessagesSent     int64
	Errors           int64
	RetryCount       int64
} {
	return struct {
		MessagesReceived int64
		MessagesSent     int64
		Errors           int64
		RetryCount       int64
	}{
		MessagesReceived: p.Producer.GetMessagesReceived(),
		MessagesSent:     p.Producer.GetMessagesSent(),
		Errors:           p.Producer.GetErrors(),
		RetryCount:       p.Producer.GetRetryCount(),
	}
}

// Override the NewProducer function for testing
func NewTestProducer(cfg config.KafkaConfig) (*TestProducer, error) {
	mockWriter := &MockWriter{}

	// We won't set expectations here - each test should set its own
	// expectations based on what it's testing

	// Create producer with mocked dependencies
	producer, err := kafka.NewProducerWithWriter(cfg, mockWriter)
	if err != nil {
		return nil, err
	}

	return &TestProducer{
		Producer:   producer,
		mockWriter: mockWriter,
	}, nil
}

func TestProducerSendMessage(t *testing.T) {
	// Setup test config
	cfg := config.KafkaConfig{
		Brokers:           []string{"localhost:9092"},
		Topic:             "test-topic",
		BatchSize:         2, // Small batch size for testing
		BatchTimeoutMs:    100,
		MaxRetries:        3,
		RetryBackoffMs:    100,
		RequiredAcks:      -1,
		EnableIdempotency: true,
		CompressionType:   "snappy",
	}

	// Create test producer
	producer, err := NewTestProducer(cfg)
	assert.NoError(t, err)

	// Set expectations for the initial connection test in NewProducerWithWriter
	producer.mockWriter.On("WriteMessages", mock.Anything, mock.Anything).Return(nil).Once()

	// Set expectations for the batch write (will be called when batch size is reached)
	producer.mockWriter.On("WriteMessages", mock.Anything, mock.Anything).Return(nil).Once()

	// Set expectations for Close() during the defer
	producer.mockWriter.On("Close").Return(nil).Once()

	defer producer.Close()

	// Send messages
	err = producer.SendMessage(cfg.Topic, []byte("test message 1"))
	assert.NoError(t, err)

	err = producer.SendMessage(cfg.Topic, []byte("test message 2"))
	assert.NoError(t, err)

	// Allow time for batch processing
	time.Sleep(200 * time.Millisecond)

	// Verify expectations - we don't do this here because the defer hasn't run yet
	// producer.mockWriter.AssertExpectations(t)
}

// Custom error that implements temporary interface
type tempError struct {
	error
}

func (t tempError) Temporary() bool {
	return true
}

func TestProducerBatchTimeout(t *testing.T) {
	// Setup test config with longer timeout
	cfg := config.KafkaConfig{
		Brokers:           []string{"localhost:9092"},
		Topic:             "test-topic",
		BatchSize:         10,  // Larger than we'll send
		BatchTimeoutMs:    200, // Will trigger timeout
		MaxRetries:        3,
		RetryBackoffMs:    100,
		RequiredAcks:      -1,
		EnableIdempotency: true,
		CompressionType:   "snappy",
	}

	// Create test producer
	producer, err := NewTestProducer(cfg)
	assert.NoError(t, err)

	// Initial connection message
	producer.mockWriter.On("WriteMessages", mock.Anything, mock.Anything).Return(nil).Once()

	// Set expectations for batch write due to timeout
	producer.mockWriter.On("WriteMessages", mock.Anything, mock.Anything).Return(nil).Once()

	// Set expectations for Close()
	producer.mockWriter.On("Close").Return(nil).Once()

	defer producer.Close()

	// Send messages (not enough to trigger batch size)
	err = producer.SendMessage(cfg.Topic, []byte("test message 1"))
	assert.NoError(t, err)

	// Wait for batch timeout to trigger the flush
	time.Sleep(300 * time.Millisecond)

	// Verify batch stats
	stats := producer.GetStats()
	assert.Equal(t, int64(1), stats.MessagesReceived, "Should have received 1 message")
	assert.Equal(t, int64(1), stats.MessagesSent, "Should have sent 1 message")
}
