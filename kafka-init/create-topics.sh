#!/bin/bash
set -euxo pipefail

# Wait for Kafka to be ready
echo "Waiting for Kafka to be ready..."
# Use kafka-topics command with a timeout to check for Kafka readiness
# Adjust the timeout and attempts as needed
until kafka-topics --bootstrap-server $KAFKA_BROKER --list > /dev/null 2>&1; do
  echo "Kafka not ready, retrying in 5 seconds..."
  sleep 5
done

echo "Kafka is ready. Creating topics..."
sleep 2 # Add a small delay just in case

# List of topics to create
TOPICS=(
  "album-created"
  "order-created"      # Renamed from order-confirmations
  "order-succeeded"    # Added for successful orders
  "order-failed"       # Added for failed orders
  # Add other topics if needed
)

for TOPIC in "${TOPICS[@]}"; do
  echo "Checking if topic '$TOPIC' exists..."
  # Check exit code of describe directly in the if condition.
  # Redirect stderr (2) to /dev/null to suppress the expected error when topic doesn't exist.
  if kafka-topics --bootstrap-server $KAFKA_BROKER --describe --topic $TOPIC 2>/dev/null; then
    echo "Topic '$TOPIC' already exists."
  else
    # Exit code was non-zero, assume topic doesn't exist.
    echo "Creating topic '$TOPIC'..."
    # set -e will cause script to exit if create fails for unexpected reasons.
    kafka-topics --bootstrap-server $KAFKA_BROKER --create --topic $TOPIC --partitions 1 --replication-factor 1
    echo "Topic '$TOPIC' created successfully."
    # Removed explicit check on $? after create, rely on set -e
  fi
done

echo "Topic creation process finished."
