#!/bin/bash

# Exit on error
set -e

echo "Starting OptionStream Producer"
echo "=============================="

# Check if Docker is running
if ! docker info > /dev/null 2>&1; then
  echo "Error: Docker is not running or not installed."
  echo "Please make sure Docker is running before proceeding."
  exit 1
fi

echo "Building Docker images..."
make build

echo "Starting Docker containers..."
make up

echo "Creating Kafka topic..."
make create-topics

echo "Starting orderbook application..."
make run

echo "OptionStream Producer is now running. Press Ctrl+C to stop."