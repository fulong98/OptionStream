.PHONY: build up down restart logs clean create-topics list-topics describe-topic consume-topic produce-test-message help run

# Default Kafka topic configuration
TOPIC_NAME ?= orderbook.deribit.options
PARTITIONS ?= 64
REPLICATION_FACTOR ?= 1

# Default container and image names
APP_CONTAINER = orderbook
KAFKA_CONTAINER = kafka
KAFKA_UI_CONTAINER = kafka-ui

help:
	@echo "Available commands:"
	@echo "  make build           - Build the Docker images"
	@echo "  make up              - Start all containers"
	@echo "  make down            - Stop and remove all containers"
	@echo "  make restart         - Restart all containers"
	@echo "  make logs            - View logs from all containers"
	@echo "  make logs-app        - View logs from the orderbook application"
	@echo "  make logs-kafka      - View logs from the Kafka broker"
	@echo "  make create-topics   - Create Kafka topics defined in config"
	@echo "  make list-topics     - List all Kafka topics"
	@echo "  make describe-topic  - Describe a specific topic (use TOPIC_NAME=your-topic)"
	@echo "  make consume-topic   - Consume messages from a topic (use TOPIC_NAME=your-topic)"
	@echo "  make produce-test-message - Send a test message to a topic (use TOPIC_NAME=your-topic)"
	@echo "  make clean           - Remove all containers, volumes, and images"
	@echo "  make run             - Run the orderbook application"

build:
	@echo "Building Docker images..."
	docker compose build

up:
	@echo "Starting containers..."
	docker compose up -d
	@echo "Waiting for Kafka to be ready..."
	@sleep 10
	@echo "All services should now be running."
	@echo "Kafka UI available at: http://localhost:8080"

down:
	@echo "Stopping containers..."
	docker compose down

restart: down up

logs:
	docker compose logs -f

logs-app:
	docker compose logs -f $(APP_CONTAINER)

logs-kafka:
	docker compose logs -f $(KAFKA_CONTAINER)

create-topics:
	@echo "Creating Kafka topic: $(TOPIC_NAME) with $(PARTITIONS) partitions and replication factor $(REPLICATION_FACTOR)"
	docker exec -it $(KAFKA_CONTAINER) kafka-topics.sh \
		--create \
		--if-not-exists \
		--topic $(TOPIC_NAME) \
		--partitions $(PARTITIONS) \
		--replication-factor $(REPLICATION_FACTOR) \
		--bootstrap-server localhost:9092
	@echo "Topic created successfully"

list-topics:
	@echo "Listing all Kafka topics..."
	docker exec -it $(KAFKA_CONTAINER) kafka-topics.sh \
		--list \
		--bootstrap-server localhost:9092

describe-topic:
	@echo "Describing topic: $(TOPIC_NAME)"
	docker exec -it $(KAFKA_CONTAINER) kafka-topics.sh \
		--describe \
		--topic $(TOPIC_NAME) \
		--bootstrap-server localhost:9092

consume-topic:
	@echo "Consuming messages from topic: $(TOPIC_NAME)"
	docker exec -it $(KAFKA_CONTAINER) kafka-console-consumer.sh \
		--topic $(TOPIC_NAME) \
		--from-beginning \
		--bootstrap-server localhost:9092 \
		--property print.key=true \
		--property print.partition=true \
		--property print.timestamp=true

produce-test-message:
	@echo "Sending a test message to topic: $(TOPIC_NAME)"
	docker exec -it $(KAFKA_CONTAINER) bash -c "echo '{\"test\":\"message\", \"timestamp\":'$$(date +%s)'}' | kafka-console-producer.sh --topic $(TOPIC_NAME) --bootstrap-server localhost:9092"
	@echo "Message sent. Use 'make consume-topic TOPIC_NAME=$(TOPIC_NAME)' to view it."

setup-multiple-topics:
	@echo "Creating multiple topics for BTC options..."
	$(MAKE) create-topics TOPIC_NAME=orderbook.deribit.btc.daily PARTITIONS=4
	$(MAKE) create-topics TOPIC_NAME=orderbook.deribit.btc.weekly PARTITIONS=4 
	$(MAKE) create-topics TOPIC_NAME=orderbook.deribit.btc.monthly PARTITIONS=4
	@echo "All topics created successfully"

clean:
	@echo "Removing all containers, volumes, and networks..."
	docker compose down -v
	@echo "Cleanup complete"

run:
	go run cmd/orderbook/main.go