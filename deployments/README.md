# Production Deployments

Standalone Dockerfiles for all pipeline services. Use when you need to run individual components against your own infrastructure.

## Services

| Dockerfile                   | Base image                              | Port(s)    |
|------------------------------|-----------------------------------------|------------|
| `zookeeper.Dockerfile`       | confluentinc/cp-zookeeper:7.4.0         | 2181       |
| `kafka.Dockerfile`           | confluentinc/cp-kafka:7.4.0             | 9092, 9101 |
| `clickhouse.Dockerfile`      | clickhouse/clickhouse-server:23.8       | 8123, 9000 |
| `mysql.Dockerfile`           | mariadb:10.11                           | 3306       |
| `debezium.Dockerfile`        | debezium/connect:2.4                    | 8083       |
| `clickhouse-sink.Dockerfile` | altinity/clickhouse-sink-connector:2.8.0-kafka | 8083 |
| `kafka-ui.Dockerfile`        | kafbat/kafka-ui:main                    | 8080       |

## Build

Image tags match `docker-compose.yml` (`*:local`). From project root:

```bash
docker build -t db-stream-zookeeper:local       -f deployments/zookeeper.Dockerfile .
docker build -t db-stream-kafka:local            -f deployments/kafka.Dockerfile .
docker build -t db-stream-clickhouse:local       -f deployments/clickhouse.Dockerfile .
docker build -t db-stream-mysql:local            -f deployments/mysql.Dockerfile .
docker build -t db-stream-debezium:local          -f deployments/debezium.Dockerfile .
docker build -t db-stream-clickhouse-sink:local  -f deployments/clickhouse-sink.Dockerfile .
docker build -t db-stream-kafka-ui:local         -f deployments/kafka-ui.Dockerfile .
```

For amd64 production (e.g. from arm64 Mac): add `--platform linux/amd64` to the clickhouse-sink build.

## Run

Override env vars for your infrastructure endpoints.

### Zookeeper (port 2181)

```bash
docker run -d --name db-stream-zookeeper -p 2181:2181 \
  db-stream-zookeeper:local
```

### Kafka (port 9092)

```bash
docker run -d --name db-stream-kafka -p 9092:9092 \
  -e KAFKA_ZOOKEEPER_CONNECT=zookeeper.your-infra:2181 \
  -e KAFKA_ADVERTISED_LISTENERS=PLAINTEXT://kafka:29092,PLAINTEXT_HOST://localhost:9092 \
  db-stream-kafka:local
```

### ClickHouse (ports 8123, 9000)

```bash
docker run -d --name db-stream-clickhouse -p 8123:8123 -p 9000:9000 \
  -e AWS_ACCESS_KEY_ID=your-key \
  -e AWS_SECRET_ACCESS_KEY=your-secret \
  db-stream-clickhouse:local
```

### MySQL (port 3306)

```bash
docker run -d --name db-stream-mysql -p 3307:3306 \
  -e MYSQL_ROOT_PASSWORD=root_password \
  db-stream-mysql:local
```

### Debezium (port 8083)

```bash
docker run -d --name db-stream-debezium -p 8083:8083 \
  -e BOOTSTRAP_SERVERS=kafka.your-infra:9092 \
  db-stream-debezium:local
```

### ClickHouse Sink (port 8084)

```bash
docker run -d --name db-stream-clickhouse-sink -p 8084:8083 \
  -e BOOTSTRAP_SERVERS=kafka.your-infra:9092 \
  -e CLICKHOUSE_SERVER_URL=http://clickhouse.your-infra:8123 \
  --add-host=host.docker.internal:host-gateway \
  db-stream-clickhouse-sink:local
```

### Kafka UI (port 8091)

```bash
docker run -d --name db-stream-kafka-ui -p 8091:8080 \
  -e KAFKA_CLUSTERS_0_BOOTSTRAPSERVERS=kafka.your-infra:9092 \
  -e KAFKA_CLUSTERS_0_ZOOKEEPER=zookeeper.your-infra:2181 \
  --restart unless-stopped \
  db-stream-kafka-ui:local
```

## Resource Limits (from docker-compose)

| Service         | mem_limit |
|-----------------|-----------|
| kafka           | 1g        |
| mysql           | 256m      |
| debezium        | 512m      |
| clickhouse-sink | 2g        |
| zookeeper       | (none)    |
| clickhouse      | (none)    |
| kafka-ui        | (none)    |

Add with `-m 256m` etc. if desired.
