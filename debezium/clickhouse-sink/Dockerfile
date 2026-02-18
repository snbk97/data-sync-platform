FROM altinity/clickhouse-sink-connector:2.9.0-lt

# Reduce plugin scan overhead at Kafka Connect startup.
# These artifacts are not needed at runtime for the MySQL -> ClickHouse sink flow.
RUN rm -f /kafka/connect/clickhouse-kafka-sink-connector/*-sources.jar \
    /kafka/connect/clickhouse-kafka-sink-connector/apicurio.tgz
