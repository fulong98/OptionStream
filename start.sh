#!/bin/bash

# Exit on error
set -e

echo "Starting OptionStream System"
echo "==========================="

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

# Build the application
echo "Building the orderbook application..."
go build -o optionstream cmd/orderbook/main.go

# Start producer in background
echo "Starting orderbook producer in background..."
./optionstream &
PRODUCER_PID=$!

# Start dashboard
echo "Starting dashboard application..."
go run cmd/dashboard/main.go &
DASHBOARD_PID=$!

# Set up trap to kill both processes on exit
trap "echo 'Shutting down...'; kill $PRODUCER_PID $DASHBOARD_PID; make down; echo 'OptionStream stopped.';" INT TERM

echo "OptionStream system is now running."
echo "- Kafka UI available at: http://localhost:8080"
echo "- Dashboard available at: http://localhost:8081"
echo "Press Ctrl+C to stop all components."

# Wait for signal
wait