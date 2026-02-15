package db

import (
	"database/sql"
	"fmt"

	_ "github.com/go-sql-driver/mysql"
)

// MySQLInspector implements Inspector for MySQL databases
type MySQLInspector struct {
	db *sql.DB
}

// NewMySQLInspector creates a new MySQL database inspector
func NewMySQLInspector(dsn string) (*MySQLInspector, error) {
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	return &MySQLInspector{db: db}, nil
}

// Close closes the database connection
func (i *MySQLInspector) Close() error {
	return i.db.Close()
}

// ListDatabases returns all databases on the MySQL server
func (i *MySQLInspector) ListDatabases() ([]Database, error) {
	query := `
		SELECT 
			SCHEMA_NAME, 
			DEFAULT_CHARACTER_SET_NAME, 
			DEFAULT_COLLATION_NAME 
		FROM information_schema.SCHEMATA 
		WHERE SCHEMA_NAME NOT IN (
			'mysql', 'information_schema', 'performance_schema', 'sys'
		)
		ORDER BY SCHEMA_NAME
	`

	rows, err := i.db.Query(query)
	if err != nil {
		return nil, fmt.Errorf("failed to query databases: %w", err)
	}
	defer rows.Close()

	var databases []Database
	for rows.Next() {
		var db Database
		if err := rows.Scan(&db.Name, &db.Charset, &db.Collation); err != nil {
			return nil, fmt.Errorf("failed to scan database: %w", err)
		}
		databases = append(databases, db)
	}

	return databases, rows.Err()
}

// ListTables returns all tables in a given database
func (i *MySQLInspector) ListTables(database string) ([]Table, error) {
	query := `
		SELECT 
			TABLE_NAME,
			ENGINE,
			TABLE_ROWS,
			DATA_LENGTH + INDEX_LENGTH AS TOTAL_SIZE
		FROM information_schema.TABLES 
		WHERE TABLE_SCHEMA = ? 
			AND TABLE_TYPE = 'BASE TABLE'
		ORDER BY TABLE_NAME
	`

	rows, err := i.db.Query(query, database)
	if err != nil {
		return nil, fmt.Errorf("failed to query tables: %w", err)
	}
	defer rows.Close()

	var tables []Table
	for rows.Next() {
		var t Table
		if err := rows.Scan(&t.Name, &t.Engine, &t.Rows, &t.DataSize); err != nil {
			return nil, fmt.Errorf("failed to scan table: %w", err)
		}
		t.IndexSize = t.DataSize
		tables = append(tables, t)
	}

	return tables, rows.Err()
}

// GetTableDetails returns detailed information about a specific table
func (i *MySQLInspector) GetTableDetails(database, table string) (map[string]interface{}, error) {
	query := `
		SELECT 
			TABLE_NAME,
			ENGINE,
			TABLE_ROWS,
			DATA_LENGTH,
			INDEX_LENGTH,
			TABLE_COLLATION,
			CREATE_OPTIONS
		FROM information_schema.TABLES 
		WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ?
	`

	details := make(map[string]interface{})

	var name, engine, collation, createOptions string
	var rowCount, dataSize, indexSize int64

	err := i.db.QueryRow(query, database, table).Scan(
		&name, &engine, &rowCount, &dataSize, &indexSize, &collation, &createOptions,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to get table details: %w", err)
	}

	details["name"] = name
	details["engine"] = engine
	details["rows"] = rowCount
	details["data_size"] = dataSize
	details["index_size"] = indexSize
	details["total_size"] = dataSize + indexSize
	details["collation"] = collation
	details["create_options"] = createOptions

	return details, nil
}
