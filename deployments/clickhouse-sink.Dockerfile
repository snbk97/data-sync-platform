# Altinity ClickHouse Kafka Connect Sink - Kafka -> ClickHouse
# Build: docker build -t db-stream-clickhouse-sink -f deployments/clickhouse-sink.Dockerfile .
# Run: see deployments/README.md

FROM altinity/clickhouse-sink-connector:2.8.0-kafka

# Reduce plugin scan overhead at Kafka Connect startup.
# Remove unused artifacts (sources, apicurio) not needed for sink flow.
RUN rm -f /kafka/connect/clickhouse-kafka-sink-connector/*-sources.jar \
    /kafka/connect/clickhouse-kafka-sink-connector/apicurio.tgz

# Kafka Connect config
ENV KAFKA_HEAP_OPTS="-Xms1G -Xmx2G" \
    GROUP_ID=clickhouse-sink-connect-cluster \
    CONFIG_STORAGE_TOPIC=clickhouse_sink_connect_configs \
    OFFSET_STORAGE_TOPIC=clickhouse_sink_connect_offsets \
    STATUS_STORAGE_TOPIC=clickhouse_sink_connect_statuses \
    CONFIG_STORAGE_REPLICATION_FACTOR=1 \
    OFFSET_STORAGE_REPLICATION_FACTOR=1 \
    STATUS_STORAGE_REPLICATION_FACTOR=1

# ClickHouse sink config
ENV CLICKHOUSE_SERVER_URL=http://clickhouse:8123 \
    CLICKHOUSE_SERVER_USER=default \
    CLICKHOUSE_SERVER_PASSWORD=
ENV CLICKHOUSE_SERVER_DATABASE=mysql_employee_mirror \
    BOOTSTRAP_SERVERS=kafka:29092 \
    KAFKA_BROKERS=kafka:29092 \
    CLICKHOUSE_TOPICS_REGEX='(pg_orion_mirror|pg_sirius_mirror)\..*' \
    REPLACINGMERGETREE_DELETE_COLUMN=__deleted \
    AUTO_CREATE_TABLES=true \
    SCHEMA_EVOLUTION=true \
    MAP_JSON_AS_STRING=true \
    MAP_UUID_AS_STRING=true

EXPOSE 8083