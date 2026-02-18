package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/joho/godotenv"
	"github.com/segmentio/kafka-go"
)

const (
	defaultSchemaHistoryRegex = `.*\.schema-history$`
	defaultGroupID            = "schema-history-clickhouse-applier"
)

var (
	decimalExprPattern   = regexp.MustCompile(`\((\d+)\s*,\s*(\d+)\)`)
	singleIntExprPattern = regexp.MustCompile(`\((\d+)\)`)
)

type appConfig struct {
	KafkaBrokers            []string
	KafkaGroupID            string
	KafkaStartOffset        int64
	CommitOnError           bool
	SchemaHistoryTopics     []string
	SchemaHistoryTopicRegex *regexp.Regexp
	TopicDiscoveryInterval  time.Duration

	ClickHouseHTTPURL     string
	ClickHouseUser        string
	ClickHousePassword    string
	ClickHouseHTTPTimeout time.Duration

	SourceToMirrorDB map[string]string
	DryRun           bool
	AllowDropColumns bool
}

type service struct {
	cfg        appConfig
	clickhouse *clickHouseClient

	topicMu      sync.Mutex
	topicCancels map[string]context.CancelFunc
}

type clickHouseClient struct {
	baseURL  string
	user     string
	password string
	client   *http.Client
}

type schemaHistoryMessage struct {
	Source       sourceMetadata `json:"source"`
	DDL          string         `json:"ddl"`
	TableChanges []tableChange  `json:"tableChanges"`
}

type sourceMetadata struct {
	Server string `json:"server"`
}

type tableChange struct {
	Type  string           `json:"type"`
	ID    string           `json:"id"`
	Table *tableDefinition `json:"table"`
}

type tableDefinition struct {
	Columns []tableColumn `json:"columns"`
}

type tableColumn struct {
	Name           string `json:"name"`
	TypeName       string `json:"typeName"`
	TypeExpression string `json:"typeExpression"`
	Optional       bool   `json:"optional"`
	Length         int    `json:"length"`
	Scale          int    `json:"scale"`
}

func main() {
	cfg, err := loadConfig()
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	clickhouse, err := newClickHouseClient(cfg)
	if err != nil {
		log.Fatalf("failed to initialize ClickHouse client: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	svc := &service{
		cfg:          cfg,
		clickhouse:   clickhouse,
		topicCancels: make(map[string]context.CancelFunc),
	}

	log.Printf(
		"schema history applier starting (brokers=%v, group=%s, clickhouse_url=%s, dry_run=%t, allow_drop_columns=%t)",
		cfg.KafkaBrokers,
		cfg.KafkaGroupID,
		cfg.ClickHouseHTTPURL,
		cfg.DryRun,
		cfg.AllowDropColumns,
	)

	if err := svc.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		log.Fatalf("service stopped with error: %v", err)
	}

	log.Printf("schema history applier stopped")
}

func loadConfig() (appConfig, error) {
	_ = godotenv.Load()

	brokers := splitCSV(getEnv("KAFKA_BROKERS", "localhost:9092"))
	if len(brokers) == 0 {
		return appConfig{}, errors.New("KAFKA_BROKERS is empty")
	}

	var kafkaStartOffset int64
	switch strings.ToLower(getEnv("KAFKA_START_OFFSET", "earliest")) {
	case "earliest", "first":
		kafkaStartOffset = kafka.FirstOffset
	case "latest", "last":
		kafkaStartOffset = kafka.LastOffset
	default:
		return appConfig{}, fmt.Errorf("invalid KAFKA_START_OFFSET, expected earliest/latest")
	}

	topicRegexText := getEnv("SCHEMA_HISTORY_TOPIC_REGEX", defaultSchemaHistoryRegex)
	topicRegex, err := regexp.Compile(topicRegexText)
	if err != nil {
		return appConfig{}, fmt.Errorf("invalid SCHEMA_HISTORY_TOPIC_REGEX: %w", err)
	}

	discoveryInterval, err := parseDurationEnv("TOPIC_DISCOVERY_INTERVAL", 30*time.Second)
	if err != nil {
		return appConfig{}, err
	}

	clickhouseHTTPTimeout, err := parseDurationEnv("CLICKHOUSE_HTTP_TIMEOUT", 10*time.Second)
	if err != nil {
		return appConfig{}, err
	}

	clickhouseHTTPURL, err := resolveClickHouseHTTPURL()
	if err != nil {
		return appConfig{}, err
	}

	cfg := appConfig{
		KafkaBrokers:            brokers,
		KafkaGroupID:            getEnv("KAFKA_GROUP_ID", defaultGroupID),
		KafkaStartOffset:        kafkaStartOffset,
		CommitOnError:           getEnvBool("COMMIT_ON_ERROR", true),
		SchemaHistoryTopics:     splitCSV(getEnv("SCHEMA_HISTORY_TOPICS", "")),
		SchemaHistoryTopicRegex: topicRegex,
		TopicDiscoveryInterval:  discoveryInterval,
		ClickHouseHTTPURL:       clickhouseHTTPURL,
		ClickHouseUser:          os.Getenv("CLICKHOUSE_USER"),
		ClickHousePassword:      os.Getenv("CLICKHOUSE_PASSWORD"),
		ClickHouseHTTPTimeout:   clickhouseHTTPTimeout,
		SourceToMirrorDB:        parseMapping(getEnv("SOURCE_TO_MIRROR_DB_MAP", "")),
		DryRun:                  getEnvBool("DRY_RUN", false),
		AllowDropColumns:        getEnvBool("ALLOW_DROP_COLUMNS", false),
	}

	if cfg.ClickHouseUser == "" {
		cfg.ClickHouseUser = "default"
	}

	return cfg, nil
}

func resolveClickHouseHTTPURL() (string, error) {
	if raw := strings.TrimSpace(os.Getenv("CLICKHOUSE_HTTP_URL")); raw != "" {
		if _, err := url.ParseRequestURI(raw); err != nil {
			return "", fmt.Errorf("invalid CLICKHOUSE_HTTP_URL: %w", err)
		}
		return raw, nil
	}

	host := getEnv("CLICKHOUSE_HOST", "localhost")
	httpPort := getEnv("CLICKHOUSE_HTTP_PORT", "8123")
	rawURL := fmt.Sprintf("http://%s:%s", host, httpPort)

	if _, err := url.ParseRequestURI(rawURL); err != nil {
		return "", fmt.Errorf("invalid generated ClickHouse URL %q: %w", rawURL, err)
	}

	return rawURL, nil
}

func newClickHouseClient(cfg appConfig) (*clickHouseClient, error) {
	client := &clickHouseClient{
		baseURL:  cfg.ClickHouseHTTPURL,
		user:     cfg.ClickHouseUser,
		password: cfg.ClickHousePassword,
		client: &http.Client{
			Timeout: cfg.ClickHouseHTTPTimeout,
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), cfg.ClickHouseHTTPTimeout)
	defer cancel()

	if _, err := client.queryLines(ctx, "SELECT 1 FORMAT TabSeparated"); err != nil {
		return nil, fmt.Errorf("clickhouse ping failed: %w", err)
	}

	return client, nil
}

func (c *clickHouseClient) exec(ctx context.Context, query string) error {
	_, err := c.sendQuery(ctx, query)
	return err
}

func (c *clickHouseClient) queryLines(ctx context.Context, query string) ([]string, error) {
	body, err := c.sendQuery(ctx, query)
	if err != nil {
		return nil, err
	}

	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 {
		return nil, nil
	}

	rawLines := strings.Split(string(trimmed), "\n")
	lines := make([]string, 0, len(rawLines))
	for _, line := range rawLines {
		line = strings.TrimSpace(line)
		if line != "" {
			lines = append(lines, line)
		}
	}

	return lines, nil
}

func (c *clickHouseClient) queryUInt64(ctx context.Context, query string) (uint64, error) {
	lines, err := c.queryLines(ctx, query)
	if err != nil {
		return 0, err
	}
	if len(lines) == 0 {
		return 0, nil
	}

	value, err := strconv.ParseUint(lines[0], 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse uint64 response %q: %w", lines[0], err)
	}

	return value, nil
}

func (c *clickHouseClient) sendQuery(ctx context.Context, query string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL, strings.NewReader(query))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "text/plain")
	if c.user != "" {
		req.SetBasicAuth(c.user, c.password)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode >= http.StatusBadRequest {
		return nil, fmt.Errorf("clickhouse http %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	return body, nil
}

func (s *service) Run(ctx context.Context) error {
	if len(s.cfg.SchemaHistoryTopics) > 0 {
		s.ensureTopicConsumers(ctx, s.cfg.SchemaHistoryTopics)
		<-ctx.Done()
		s.stopAllConsumers()
		return nil
	}

	s.refreshTopicConsumers(ctx)

	ticker := time.NewTicker(s.cfg.TopicDiscoveryInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			s.stopAllConsumers()
			return nil
		case <-ticker.C:
			s.refreshTopicConsumers(ctx)
		}
	}
}

func (s *service) refreshTopicConsumers(ctx context.Context) {
	topics, err := discoverSchemaHistoryTopics(s.cfg.KafkaBrokers, s.cfg.SchemaHistoryTopicRegex)
	if err != nil {
		log.Printf("topic discovery failed: %v", err)
		return
	}
	if len(topics) == 0 {
		log.Printf("topic discovery found no schema-history topics")
		return
	}

	s.ensureTopicConsumers(ctx, topics)
}

func discoverSchemaHistoryTopics(brokers []string, topicRegex *regexp.Regexp) ([]string, error) {
	var lastErr error

	for _, broker := range brokers {
		conn, err := kafka.Dial("tcp", broker)
		if err != nil {
			lastErr = err
			continue
		}

		partitions, err := conn.ReadPartitions()
		_ = conn.Close()
		if err != nil {
			lastErr = err
			continue
		}

		topicSet := make(map[string]struct{})
		for _, partition := range partitions {
			topic := partition.Topic
			if topic == "" || strings.HasPrefix(topic, "__") {
				continue
			}
			if topicRegex.MatchString(topic) {
				topicSet[topic] = struct{}{}
			}
		}

		topics := make([]string, 0, len(topicSet))
		for topic := range topicSet {
			topics = append(topics, topic)
		}
		sort.Strings(topics)

		return topics, nil
	}

	if lastErr != nil {
		return nil, fmt.Errorf("could not read Kafka partitions: %w", lastErr)
	}

	return nil, nil
}

func (s *service) ensureTopicConsumers(parentCtx context.Context, topics []string) {
	topics = normalizeTopics(topics)

	s.topicMu.Lock()
	defer s.topicMu.Unlock()

	for _, topic := range topics {
		if _, exists := s.topicCancels[topic]; exists {
			continue
		}

		topicCtx, cancel := context.WithCancel(parentCtx)
		s.topicCancels[topic] = cancel
		go s.consumeTopic(topicCtx, topic)

		log.Printf("started consumer for topic=%s", topic)
	}
}

func (s *service) stopAllConsumers() {
	s.topicMu.Lock()
	defer s.topicMu.Unlock()

	for topic, cancel := range s.topicCancels {
		cancel()
		delete(s.topicCancels, topic)
	}
}

func (s *service) consumeTopic(ctx context.Context, topic string) {
	reader := kafka.NewReader(kafka.ReaderConfig{
		Brokers:         s.cfg.KafkaBrokers,
		GroupID:         s.cfg.KafkaGroupID,
		Topic:           topic,
		StartOffset:     s.cfg.KafkaStartOffset,
		MinBytes:        1,
		MaxBytes:        10e6,
		MaxWait:         2 * time.Second,
		ReadLagInterval: -1,
	})
	defer reader.Close()

	for {
		message, err := reader.FetchMessage(ctx)
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, context.Canceled) {
				return
			}

			log.Printf("topic=%s fetch error: %v", topic, err)
			time.Sleep(1 * time.Second)
			continue
		}

		processErr := s.processMessage(ctx, topic, message.Value)
		if processErr != nil {
			log.Printf("topic=%s offset=%d process error: %v", topic, message.Offset, processErr)
		}

		if processErr == nil || s.cfg.CommitOnError {
			if err := reader.CommitMessages(ctx, message); err != nil {
				if ctx.Err() != nil || errors.Is(err, context.Canceled) {
					return
				}

				log.Printf("topic=%s offset=%d commit error: %v", topic, message.Offset, err)
			}
		}
	}
}

func (s *service) processMessage(ctx context.Context, topic string, value []byte) error {
	if len(strings.TrimSpace(string(value))) == 0 {
		return nil
	}

	var event schemaHistoryMessage
	if err := json.Unmarshal(value, &event); err != nil {
		return fmt.Errorf("unmarshal message: %w", err)
	}

	if len(event.TableChanges) == 0 {
		return nil
	}

	sourceName := strings.TrimSpace(event.Source.Server)
	if sourceName == "" {
		sourceName = deriveSourceNameFromTopic(topic)
	}

	targetDatabase := resolveTargetDatabase(sourceName, s.cfg.SourceToMirrorDB)
	if targetDatabase == "" {
		return fmt.Errorf("target database is empty for source=%q", sourceName)
	}

	for _, change := range event.TableChanges {
		if err := s.applyTableChange(ctx, targetDatabase, change); err != nil {
			return err
		}
	}

	return nil
}

func (s *service) applyTableChange(ctx context.Context, targetDatabase string, change tableChange) error {
	changeType := strings.ToUpper(strings.TrimSpace(change.Type))
	switch changeType {
	case "CREATE", "ALTER":
		// Supported operations
	case "DROP":
		log.Printf("skip DROP change for id=%s", change.ID)
		return nil
	default:
		return nil
	}

	if change.Table == nil || len(change.Table.Columns) == 0 {
		return nil
	}

	tableName := parseTableName(change.ID)
	if tableName == "" {
		return fmt.Errorf("could not parse table name from change id=%q", change.ID)
	}

	tableExists, err := s.tableExists(ctx, targetDatabase, tableName)
	if err != nil {
		return err
	}
	if !tableExists {
		log.Printf("skip change for %s.%s: table not found in ClickHouse", targetDatabase, tableName)
		return nil
	}

	existingColumns, err := s.fetchExistingColumns(ctx, targetDatabase, tableName)
	if err != nil {
		return err
	}

	desiredColumns := make(map[string]tableColumn)
	queries := make([]string, 0)

	for _, column := range change.Table.Columns {
		columnName := strings.TrimSpace(column.Name)
		if columnName == "" {
			continue
		}

		desiredColumns[columnName] = column
		if _, exists := existingColumns[columnName]; exists {
			continue
		}

		mappedType, mapErr := mapSchemaColumnType(column)
		if mapErr != nil {
			log.Printf(
				"type fallback for %s.%s.%s (%s): %v",
				targetDatabase,
				tableName,
				columnName,
				column.TypeName,
				mapErr,
			)
		}

		queries = append(queries, fmt.Sprintf(
			"ALTER TABLE %s.%s ADD COLUMN IF NOT EXISTS %s %s",
			quoteIdentifier(targetDatabase),
			quoteIdentifier(tableName),
			quoteIdentifier(columnName),
			mappedType,
		))
	}

	if s.cfg.AllowDropColumns && changeType == "ALTER" {
		for existingColumn := range existingColumns {
			if _, keep := desiredColumns[existingColumn]; keep {
				continue
			}
			if isReservedMirrorColumn(existingColumn) {
				continue
			}

			queries = append(queries, fmt.Sprintf(
				"ALTER TABLE %s.%s DROP COLUMN IF EXISTS %s",
				quoteIdentifier(targetDatabase),
				quoteIdentifier(tableName),
				quoteIdentifier(existingColumn),
			))
		}
	}

	if len(queries) == 0 {
		return nil
	}

	for _, query := range queries {
		if s.cfg.DryRun {
			log.Printf("[dry-run] %s", query)
			continue
		}

		if err := s.clickhouse.exec(ctx, query); err != nil {
			return fmt.Errorf("execute query %q: %w", query, err)
		}

		log.Printf("applied: %s", query)
	}

	return nil
}

func (s *service) tableExists(ctx context.Context, databaseName, tableName string) (bool, error) {
	query := fmt.Sprintf(
		"SELECT count() FROM system.tables WHERE database = %s AND name = %s FORMAT TabSeparated",
		quoteSQLString(databaseName),
		quoteSQLString(tableName),
	)

	count, err := s.clickhouse.queryUInt64(ctx, query)
	if err != nil {
		return false, fmt.Errorf("query table existence for %s.%s: %w", databaseName, tableName, err)
	}

	return count > 0, nil
}

func (s *service) fetchExistingColumns(ctx context.Context, databaseName, tableName string) (map[string]struct{}, error) {
	query := fmt.Sprintf(
		"SELECT name FROM system.columns WHERE database = %s AND table = %s ORDER BY position FORMAT TabSeparated",
		quoteSQLString(databaseName),
		quoteSQLString(tableName),
	)

	lines, err := s.clickhouse.queryLines(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("fetch existing columns for %s.%s: %w", databaseName, tableName, err)
	}

	columns := make(map[string]struct{}, len(lines))
	for _, line := range lines {
		columns[line] = struct{}{}
	}

	return columns, nil
}

func mapSchemaColumnType(column tableColumn) (string, error) {
	typeName := strings.ToUpper(strings.TrimSpace(column.TypeName))
	typeExpression := strings.ToUpper(strings.TrimSpace(column.TypeExpression))
	unsigned := strings.Contains(typeExpression, "UNSIGNED")

	var (
		mappedType string
		mapErr     error
	)

	switch typeName {
	case "BOOLEAN", "BOOL":
		mappedType = "UInt8"
	case "TINYINT":
		if strings.Contains(typeExpression, "(1)") {
			mappedType = "UInt8"
		} else if unsigned {
			mappedType = "UInt8"
		} else {
			mappedType = "Int8"
		}
	case "SMALLINT":
		if unsigned {
			mappedType = "UInt16"
		} else {
			mappedType = "Int16"
		}
	case "MEDIUMINT", "INT", "INTEGER":
		if unsigned {
			mappedType = "UInt32"
		} else {
			mappedType = "Int32"
		}
	case "BIGINT":
		if unsigned {
			mappedType = "UInt64"
		} else {
			mappedType = "Int64"
		}
	case "FLOAT":
		mappedType = "Float32"
	case "DOUBLE", "REAL":
		mappedType = "Float64"
	case "DECIMAL", "NUMERIC":
		mappedType = decimalType(typeExpression, column.Length, column.Scale)
	case "DATE":
		mappedType = "Date32"
	case "DATETIME", "TIMESTAMP":
		scale := datetimeScale(typeExpression)
		mappedType = fmt.Sprintf("DateTime64(%d)", scale)
	case "TIME":
		mappedType = "String"
	case "YEAR":
		mappedType = "UInt16"
	case "BIT":
		if strings.Contains(typeExpression, "(1)") || column.Length == 1 {
			mappedType = "UInt8"
		} else {
			mappedType = "String"
		}
	case "CHAR", "VARCHAR", "TEXT", "TINYTEXT", "MEDIUMTEXT", "LONGTEXT", "JSON", "ENUM", "SET":
		mappedType = "String"
	case "BINARY", "VARBINARY", "BLOB", "TINYBLOB", "MEDIUMBLOB", "LONGBLOB":
		mappedType = "String"
	default:
		mappedType = "String"
		mapErr = fmt.Errorf("unsupported source type %q", column.TypeName)
	}

	if column.Optional {
		mappedType = fmt.Sprintf("Nullable(%s)", mappedType)
	}

	return mappedType, mapErr
}

func decimalType(typeExpression string, defaultPrecision, defaultScale int) string {
	precision := defaultPrecision
	scale := defaultScale

	if matches := decimalExprPattern.FindStringSubmatch(typeExpression); len(matches) == 3 {
		parsedPrecision, err := strconv.Atoi(matches[1])
		if err == nil {
			precision = parsedPrecision
		}
		parsedScale, err := strconv.Atoi(matches[2])
		if err == nil {
			scale = parsedScale
		}
	}

	if precision <= 0 {
		precision = 18
	}
	if scale < 0 {
		scale = 0
	}
	if precision <= scale {
		precision = scale + 1
	}
	if precision > 76 {
		precision = 76
		if scale >= precision {
			scale = precision - 1
		}
	}

	return fmt.Sprintf("Decimal(%d,%d)", precision, scale)
}

func datetimeScale(typeExpression string) int {
	if matches := singleIntExprPattern.FindStringSubmatch(typeExpression); len(matches) == 2 {
		scale, err := strconv.Atoi(matches[1])
		if err == nil {
			if scale < 0 {
				return 0
			}
			if scale > 9 {
				return 9
			}
			return scale
		}
	}

	return 3
}

func parseTableName(tableID string) string {
	cleanID := strings.TrimSpace(tableID)
	cleanID = strings.ReplaceAll(cleanID, "\"", "")
	cleanID = strings.ReplaceAll(cleanID, "`", "")
	parts := strings.Split(cleanID, ".")
	if len(parts) == 0 {
		return ""
	}

	return strings.TrimSpace(parts[len(parts)-1])
}

func deriveSourceNameFromTopic(topic string) string {
	if strings.HasSuffix(topic, ".schema-history") {
		return strings.TrimSuffix(topic, ".schema-history")
	}

	segments := strings.Split(topic, ".")
	if len(segments) > 0 {
		return segments[0]
	}

	return topic
}

func resolveTargetDatabase(sourceName string, mapping map[string]string) string {
	if mapped, ok := mapping[sourceName]; ok {
		return mapped
	}
	return sourceName
}

func quoteIdentifier(identifier string) string {
	escaped := strings.ReplaceAll(strings.TrimSpace(identifier), "`", "``")
	return "`" + escaped + "`"
}

func quoteSQLString(value string) string {
	escaped := strings.ReplaceAll(value, "'", "''")
	return "'" + escaped + "'"
}

func normalizeTopics(topics []string) []string {
	topicSet := make(map[string]struct{})
	for _, topic := range topics {
		topic = strings.TrimSpace(topic)
		if topic == "" {
			continue
		}
		topicSet[topic] = struct{}{}
	}

	result := make([]string, 0, len(topicSet))
	for topic := range topicSet {
		result = append(result, topic)
	}
	sort.Strings(result)

	return result
}

func splitCSV(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}

	parts := strings.Split(raw, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			result = append(result, part)
		}
	}

	return result
}

func parseMapping(raw string) map[string]string {
	pairs := splitCSV(raw)
	mapping := make(map[string]string, len(pairs))

	for _, pair := range pairs {
		parts := strings.SplitN(pair, ":", 2)
		if len(parts) != 2 {
			continue
		}

		source := strings.TrimSpace(parts[0])
		target := strings.TrimSpace(parts[1])
		if source == "" || target == "" {
			continue
		}

		mapping[source] = target
	}

	return mapping
}

func isReservedMirrorColumn(columnName string) bool {
	switch strings.ToLower(strings.TrimSpace(columnName)) {
	case "_sign", "_version":
		return true
	default:
		return false
	}
}

func parseDurationEnv(key string, defaultValue time.Duration) (time.Duration, error) {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return defaultValue, nil
	}

	value, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("invalid duration for %s: %w", key, err)
	}
	if value <= 0 {
		return 0, fmt.Errorf("duration for %s must be positive", key)
	}

	return value, nil
}

func getEnv(key, defaultValue string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return defaultValue
}

func getEnvBool(key string, defaultValue bool) bool {
	raw := strings.ToLower(strings.TrimSpace(os.Getenv(key)))
	switch raw {
	case "1", "true", "yes", "y", "on":
		return true
	case "0", "false", "no", "n", "off":
		return false
	case "":
		return defaultValue
	default:
		return defaultValue
	}
}
