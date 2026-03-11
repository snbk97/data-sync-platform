# Zookeeper - coordination service for Kafka
# Build: docker build -t db-stream-zookeeper -f deployments/zookeeper.Dockerfile .
# Run: see deployments/README.md

FROM confluentinc/cp-zookeeper:7.4.0

ENV ZOOKEEPER_CLIENT_PORT=2181 \
    ZOOKEEPER_TICK_TIME=2000

EXPOSE 2181
