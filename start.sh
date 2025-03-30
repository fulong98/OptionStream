#!/bin/bash

# Exit on error
set -e

echo "Building Docker images..."
make build

echo "Starting Docker containers..."
make up

echo "Waiting for Kafka to be ready..."
sleep 10

echo "Creating Kafka topics..."
make create-topics

echo "Starting orderbook application..."
make run 