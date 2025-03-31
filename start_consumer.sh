#!/bin/bash

# Exit on error
set -e

echo "Starting OptionStream Dashboard"
echo "============================="

# Check if Kafka is running
if ! docker ps | grep -q kafka; then
  echo "Error: Kafka container is not running."
  echo "Please run ./start_producer.sh first to set up the environment."
  exit 1
fi

echo "Starting dashboard application..."
go run cmd/dashboard/main.go

echo "OptionStream Dashboard is now running. Press Ctrl+C to stop."