# Kafka UI - monitoring and management UI for Kafka
# Build: docker build -t db-stream-kafka-ui -f deployments/kafka-ui.Dockerfile .
# Run: see deployments/README.md

FROM kafbat/kafka-ui:main

# Kafka cluster config (override at runtime for production)
ENV KAFKA_CLUSTERS_0_NAME=db-stream-local \
    KAFKA_CLUSTERS_0_BOOTSTRAPSERVERS=kafka:29092 \
    KAFKA_CLUSTERS_0_ZOOKEEPER=zookeeper:2181 \
    DYNAMIC_CONFIG_ENABLED='true'

EXPOSE 8080
