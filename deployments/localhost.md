
# 1) Stop compose stack first
docker-compose down --remove-orphans

# 2) Cleanup old standalone containers (if any)
for c in db-stream-zookeeper db-stream-kafka db-stream-clickhouse db-stream-mysql db-stream-debezium db-stream-clickhouse-sink db-stream-kafka-ui; do
  docker rm -f "$c" 2>/dev/null || true
done

# 3) Create network + volumes (to match compose behavior)
docker network create db-stream-network 2>/dev/null || true
for v in zookeeper-data zookeeper-logs kafka-data clickhouse-data mysql-data; do
  docker volume create "$v" >/dev/null
done

# 4) Build each Dockerfile from deployments/
cd deployments
docker build -t db-stream-zookeeper:local       -f zookeeper.Dockerfile ..
docker build -t db-stream-kafka:local           -f kafka.Dockerfile ..
docker build -t db-stream-clickhouse:local      -f clickhouse.Dockerfile ..
docker build -t db-stream-mysql:local           -f mysql.Dockerfile ..
docker build -t db-stream-debezium:local        -f debezium.Dockerfile ..
docker build -t db-stream-clickhouse-sink:local -f clickhouse-sink.Dockerfile ..
docker build -t db-stream-kafka-ui:local        -f kafka-ui.Dockerfile ..
cd ..

# 5) Run each service individually (compose-equivalent order/settings)
docker run -d --name db-stream-zookeeper --hostname zookeeper \
  --network db-stream-network -p 2181:2181 \
  -v zookeeper-data:/var/lib/zookeeper/data \
  -v zookeeper-logs:/var/lib/zookeeper/log \
  db-stream-zookeeper:local

docker run -d --name db-stream-kafka --hostname kafka \
  --network db-stream-network -p 9092:9092 -p 9101:9101 \
  -m 1g \
  -v kafka-data:/var/lib/kafka/data \
  -e KAFKA_BROKER_ID=1 \
  -e KAFKA_ZOOKEEPER_CONNECT=zookeeper:2181 \
  -e KAFKA_LISTENER_SECURITY_PROTOCOL_MAP=PLAINTEXT:PLAINTEXT,PLAINTEXT_HOST:PLAINTEXT \
  -e KAFKA_ADVERTISED_LISTENERS=PLAINTEXT://kafka:29092,PLAINTEXT_HOST://localhost:9092 \
  -e KAFKA_OFFSETS_TOPIC_REPLICATION_FACTOR=1 \
  -e KAFKA_TRANSACTION_STATE_LOG_MIN_ISR=1 \
  -e KAFKA_TRANSACTION_STATE_LOG_REPLICATION_FACTOR=1 \
  -e KAFKA_GROUP_INITIAL_REBALANCE_DELAY_MS=0 \
  -e KAFKA_JMX_PORT=9101 \
  -e KAFKA_JMX_HOSTNAME=localhost \
  -e KAFKA_AUTO_CREATE_TOPICS_ENABLE=true \
  -e KAFKA_HEAP_OPTS="-Xms256M -Xmx512M" \
  db-stream-kafka:local

docker run -d --name db-stream-clickhouse --hostname clickhouse \
  --network db-stream-network -p 8123:8123 -p 9000:9000 \
  --env-file .env \
  -v clickhouse-data:/var/lib/clickhouse \
  -e CLICKHOUSE_DEFAULT_ACCESS_MANAGEMENT=1 \
  db-stream-clickhouse:local

docker run -d --name db-stream-mysql --hostname mysql \
  --network db-stream-network -p 3307:3306 \
  -m 256m \
  -v mysql-data:/var/lib/mysql \
  -e MYSQL_ROOT_PASSWORD=root_password \
  -e MYSQL_DATABASE=employee \
  -e MYSQL_USER=db_stream \
  -e MYSQL_PASSWORD=db_stream_password \
  db-stream-mysql:local

docker run -d --name db-stream-debezium --hostname debezium \
  --network db-stream-network -p 8083:8083 \
  -m 512m \
  -e BOOTSTRAP_SERVERS=kafka:29092 \
  -e GROUP_ID=debezium-connect-cluster \
  -e CONFIG_STORAGE_TOPIC=connect_configs \
  -e OFFSET_STORAGE_TOPIC=connect_offsets \
  -e STATUS_STORAGE_TOPIC=connect_statuses \
  -e KEY_CONVERTER=org.apache.kafka.connect.json.JsonConverter \
  -e VALUE_CONVERTER=org.apache.kafka.connect.json.JsonConverter \
  -e HEAP_OPTS="-Xms128M -Xmx256M" \
  db-stream-debezium:local

docker run --rm -d \
  --name db-stream-clickhouse-sink \
  -p 8084:8083 \
  -m 2g \
  --add-host=host.docker.internal:host-gateway \
  -e REST_HOST_NAME=0.0.0.0 \
  -e ADVERTISED_HOST_NAME=localhost \
  -e REST_PORT=8083 \
  -e ADVERTISED_PORT=8083 \
  -e KAFKA_HEAP_OPTS='-Xms512M -Xmx1024M' \
  -e GROUP_ID=clickhouse-sink-connect-cluster \
  -e CONFIG_STORAGE_TOPIC=clickhouse_sink_connect_configs \
  -e OFFSET_STORAGE_TOPIC=clickhouse_sink_connect_offsets \
  -e STATUS_STORAGE_TOPIC=clickhouse_sink_connect_statuses \
  -e CONFIG_STORAGE_REPLICATION_FACTOR=1 \
  -e OFFSET_STORAGE_REPLICATION_FACTOR=1 \
  -e STATUS_STORAGE_REPLICATION_FACTOR=1 \
  -e CLICKHOUSE_SERVER_URL=http://host.docker.internal:8123 \
  -e CLICKHOUSE_SERVER_USER=default \
  -e CLICKHOUSE_SERVER_PASSWORD= \
  -e CLICKHOUSE_SERVER_DATABASE=mysql_employee_mirror \
  -e BOOTSTRAP_SERVERS=host.docker.internal:9092 \
  -e KAFKA_BROKERS=host.docker.internal:9092 \
  -e REPLACINGMERGETREE_DELETE_COLUMN=__deleted \
  -e AUTO_CREATE_TABLES=true \
  -e SCHEMA_EVOLUTION=true \
  -e MAP_JSON_AS_STRING=true \
  -e MAP_UUID_AS_STRING=true \
  db-stream-clickhouse-sink:local

docker run -d --name db-stream-kafka-ui \
  --network db-stream-network -p 8091:8080 \
  --restart unless-stopped \
  -e KAFKA_CLUSTERS_0_NAME=db-stream-local \
  -e KAFKA_CLUSTERS_0_BOOTSTRAPSERVERS=kafka:29092 \
  -e KAFKA_CLUSTERS_0_ZOOKEEPER=zookeeper:2181 \
  -e DYNAMIC_CONFIG_ENABLED=true \
  db-stream-kafka-ui:local

# 6) Verify
docker ps --format "table {{.Names}}\t{{.Status}}\t{{.Ports}}"