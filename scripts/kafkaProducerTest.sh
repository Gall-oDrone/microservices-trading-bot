#!/bin/bash

# Check if the correct number of arguments is provided
if [ "$#" -ne 2 ]; then
    echo "Usage: $0 <topic> <broker_address:port>"
    exit 1
fi

# Extracting topic and broker information from arguments
topic="$1"
broker_address_port="$2"

# List of Kafka messages to be sent
kafkaMsgToBeSent=(
'{"Book":"btc_mxn","Payload":[{"i":1,"a":"0.355","r":"1.0","v":"177947.3","mo":"1motest","to":"1totest","t":0,"x":1707365521710}]}'
'{"Book":"btc_mxn","Payload":[{"i":2,"a":"0.355","r":"2.0","v":"177947.3","mo":"1motest","to":"1totest","t":0,"x":1707365526710}]}'
'{"Book":"btc_mxn","Payload":[{"i":3,"a":"0.355","r":"3.0","v":"177947.3","mo":"1motest","to":"1totest","t":0,"x":1707365531710}]}'
'{"Book":"btc_mxn","Payload":[{"i":4,"a":"0.355","r":"4.0","v":"177947.3","mo":"1motest","to":"1totest","t":0,"x":1707365536710}]}'
'{"Book":"btc_mxn","Payload":[{"i":5,"a":"0.355","r":"5.0","v":"177947.3","mo":"1motest","to":"1totest","t":0,"x":1707365541710}]}'
)

# Function to print progress messages
function print_progress {
    echo ">> $1"
}

# Step 3: Run Kafka Producer
print_progress "Running Kafka Producer for Topic: $topic"

# Iterate over the list and send each element to the Kafka producer
for msg in "${kafkaMsgToBeSent[@]}"; do
    echo $msg | docker-compose exec -T kafka kafka-console-producer.sh --broker-list $broker_address_port --topic $topic
done

# Additional progress message
print_progress "Producer terminated. Setup completed for topic: $topic"