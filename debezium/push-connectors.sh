#!/bin/bash

# Configuration
CONNECTORS_DIR="./debezium/connectors"
DEBEZIUM_URL="http://localhost:8083"

# Check if directory exists
if [ ! -d "$CONNECTORS_DIR" ]; then
    echo "Error: Directory $CONNECTORS_DIR not found."
    exit 1
fi

echo "--- Starting Connector Registration ---"

for file in "$CONNECTORS_DIR"/*.json; do
    # Get filename without path and extension
    name=$(basename "$file" .json)
    
    echo -n "Registering/Updating $name... "
    
    # PUT request to create or update
    response=$(curl -s -w "%{http_code}" -o /dev/null -X PUT \
         -H "Content-Type: application/json" \
         --data @"$file" \
         "$DEBEZIUM_URL/connectors/$name/config")

    if [ "$response" == "200" ] || [ "$response" == "201" ]; then
        echo "✅ SUCCESS ($response)"
    else
        echo "❌ FAILED ($response)"
        # Print error details if failed
        curl -s -X PUT -H "Content-Type: application/json" --data @"$file" "$DEBEZIUM_URL/connectors/$name/config"
    fi
done

echo "--- Finished ---"


# curl -s -X PUT -H "Content-Type: application/json" --data @debezium/connectors/mysql-employee-connector.json http://localhost:8083/connectors/mysql-employee-connector/config