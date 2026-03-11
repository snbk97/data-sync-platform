# ClickHouse - analytics database for the sink
# Build: docker build -t db-stream-clickhouse -f deployments/clickhouse.Dockerfile .
# Run: see deployments/README.md

FROM clickhouse/clickhouse-server:23.8

# ClickHouse config overlays (zookeeper coordination + experimental features)
COPY clickhouse/config.d/zookeeper.xml /etc/clickhouse-server/config.d/zookeeper.xml
COPY clickhouse/users.d/experimental.xml /etc/clickhouse-server/users.d/experimental.xml

ENV CLICKHOUSE_DEFAULT_ACCESS_MANAGEMENT=1

# AWS credentials for S3Queue/S3 (override at runtime; never bake secrets into the image)
ENV AWS_ACCESS_KEY_ID="" \
    AWS_SECRET_ACCESS_KEY="" \
    AWS_REGION="us-east-1"

EXPOSE 8123 9000
