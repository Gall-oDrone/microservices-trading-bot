#!/bin/bash

# Check if the correct number of arguments is provided
if [ "$#" -ne 2]; then
    echo "Usage: $0 <topic> <broker_address:port>"
    exit 1
fi

# Extracting topic and broker information from arguments
topic="$1"
broker_address_port="$2"

# Function to print progress messages
function print_progress {
    echo ">> $1"
}

# Step 1: Run Kafka Consumer
print_progress "Running Kafka Consumer for Topic_ $topic"

docker-compose exec -T kafka kafka-console-consumer.sh --bootstrap-server $broker_address_port --topic $topic  --from-beginning

# Additional progress message
print_progress "Consumer terminated. Completed consuming messages from topic: $topic"