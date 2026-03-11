# Debezium Kafka Connect - CDC connector for MySQL/PostgreSQL -> Kafka
# Build: docker build -t db-stream-debezium -f deployments/debezium.Dockerfile .
# Run: see deployments/README.md

FROM debezium/connect:2.4

# Kafka Connect config (override at runtime for production)
ENV BOOTSTRAP_SERVERS=kafka:29092 \
    GROUP_ID=debezium-connect-cluster \
    CONFIG_STORAGE_TOPIC=connect_configs \
    OFFSET_STORAGE_TOPIC=connect_offsets \
    STATUS_STORAGE_TOPIC=connect_statuses \
    KEY_CONVERTER=org.apache.kafka.connect.json.JsonConverter \
    VALUE_CONVERTER=org.apache.kafka.connect.json.JsonConverter \
    HEAP_OPTS="-Xms1G -Xmx2G"

EXPOSE 8083