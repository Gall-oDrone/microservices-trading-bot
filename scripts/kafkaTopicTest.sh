#!/bin/bash

# Check if the correct number of arguments is provided
if [ "$#" -ne 2 ]; then
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

# Step 1: Delete Kafka Topic
print_progress "Deleting Kafka Topic: $topic"
docker-compose exec kafka kafka-topics.sh --zookeeper zookeeper:2181 --delete --topic $topic

# Step 2: Create Kafka Topic
print_progress "Creating Kafka Topic: $topic"
docker-compose exec kafka kafka-topics.sh --zookeeper zookeeper:2181 --create --topic $topic --partitions 1 --replication-factor 1

# Additional progress message
print_progress "Creating Kafka Topic finished. Initial setup completed for topic: $topic"