#!/bin/bash

# Check if .env file exists
if [ ! -f .env ]; then
    echo "Error: .env file not found!"
    echo "Please create a .env file based on .env.example"
    exit 1
fi

# Build the Docker image
echo "Building Docker image..."
docker build -t go-air-dev .

# Run the container with hot reloading
echo "Starting container..."
docker run --rm \
    -p 8080:8080 \
    -v $(pwd):/app \
    -v $(pwd)/tmp:/tmp \
    --env-file .env \
    go-air-dev 