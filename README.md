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
```

## Setup

1. Start the Kafka stack:
```bash
docker-compose up -d
```

2. Build the application:
```bash
go build -o orderbook cmd/orderbook/main.go
```

3. Run the application:
```bash
./orderbook
```

## Monitoring

The system provides real-time monitoring through logs:
- Message throughput statistics
- Error rates and types
- Batch processing metrics
- Connection status

Example log output:
```
Kafka Stats - Received: 1000, Sent: 998, Errors: 2, Retries: 1, Last Error: connection refused, Last Batch Size: 100
```

## Performance Optimization

The system is optimized for:
- High throughput through batch processing
- Low latency with configurable batch timeouts
- Data reliability with idempotency and retries
- Resource efficiency with connection pooling
- Network resilience with automatic reconnection

## Create topic
```
docker exec -it kafka kafka-topics.sh --create --topic orderbook.deribit.options --bootstrap-server localhost:9092 --partitions 3 --replication-factor 1
```