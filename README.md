# OptionStream

A high-performance system for collecting and streaming BTC/USD options order book data from Deribit to Kafka. This system is designed for both real-time market making and historical data analysis.

## Features

- **Real-time Order Book Data Collection**
  - Subscribes to multiple BTC/USD options instruments
  - Efficient WebSocket connection management
  - Automatic reconnection and heartbeat mechanism
  - Configurable instrument list

- **High-Performance Kafka Integration**
  - Optimized for high throughput and low latency
  - Batch processing with configurable size and timeout
  - Message compression (Snappy)
  - Idempotency guarantees
  - Automatic retry mechanism
  - Detailed monitoring and statistics

- **Robust Error Handling**
  - Automatic reconnection for network issues
  - Graceful error recovery
  - Comprehensive error logging
  - Performance monitoring

## Prerequisites

- Go 1.21 or later
- Docker and Docker Compose
- Kafka cluster (can be run locally using Docker)

## Project Structure

```
.
├── cmd/
│   └── orderbook/          # Main application entry point
├── config/
│   └── config.yaml         # Configuration file
├── internal/
│   ├── config/            # Configuration management
│   ├── deribit/           # Deribit WebSocket client
│   └── kafka/             # Kafka producer implementation
├── docker-compose.yml      # Docker services configuration
├── Makefile              # Build and management commands
└── README.md
```

## Configuration

The system is configured through `config/config.yaml`:

```yaml
deribit:
  websocket_url: "wss://test.deribit.com/ws/api/v2"
  testnet: true
  instruments:
    - "BTC-30MAR25-70000-C"
    - "BTC-30MAR25-70000-P"
    # ... more instruments

kafka:
  brokers:
    - "127.0.0.1:9092"
  topic: "orderbook.deribit.options"
  batch_size: 100
  batch_timeout_ms: 100
  max_retries: 3
  retry_backoff_ms: 100
  required_acks: -1
  enable_idempotency: true
  compression_type: "snappy"

server:
  port: 8080
  metrics_path: "/metrics"
```

## Setup and Usage

### Using Makefile Commands

The project includes a comprehensive Makefile with various commands for development and management:

```bash
# Build and run
make build           # Build the Docker images
make up              # Start all containers
make down            # Stop and remove all containers
make restart         # Restart all containers

# Logging
make logs            # View logs from all containers
make logs-app        # View logs from the orderbook application
make logs-kafka      # View logs from the Kafka broker

# Kafka Management
make create-topics   # Create Kafka topics defined in config
make list-topics     # List all Kafka topics
make describe-topic  # Describe a specific topic
make consume-topic   # Consume messages from a topic
make produce-test-message  # Send a test message to a topic

# Development
make run             # Run the orderbook application locally
make clean           # Remove all containers, volumes, and images
```

### Manual Setup

1. Start the Kafka stack:
```bash
docker-compose up -d
```

2. Create the required Kafka topics:
```bash
make create-topics TOPIC_NAME=orderbook.deribit.options PARTITIONS=64
```

3. Build and run the application:
```bash
make build
make up
```

## Monitoring

The system provides real-time monitoring through:
- HTTP metrics endpoint at `/metrics`
- Detailed application logs
- Kafka UI available at http://localhost:8080

### Metrics Available
- Message throughput statistics
- Error rates and types
- Batch processing metrics
- Connection status
- WebSocket connection health

## Performance Optimization

The system is optimized for:
- High throughput through batch processing
- Low latency with configurable batch timeouts
- Data reliability with idempotency and retries
- Resource efficiency with connection pooling
- Network resilience with automatic reconnection

## Development

To run the application locally for development:
```bash
make run
```

To view logs:
```bash
make logs-app
```

To consume messages from a topic:
```bash
make consume-topic TOPIC_NAME=orderbook.deribit.options
```

## Create topic
```
docker exec -it kafka kafka-topics.sh --create --topic orderbook.deribit.options --bootstrap-server localhost:9092 --partitions 64 --replication-factor 1
```


kafka-console-consumer.sh --bootstrap-server localhost:9092 --topic orderbook.deribit.options --from-beginning