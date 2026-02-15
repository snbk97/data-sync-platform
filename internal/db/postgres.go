package db

import (
	"database/sql"
	"fmt"

	_ "github.com/lib/pq"
)

// PostgresInspector implements Inspector for PostgreSQL databases
type PostgresInspector struct {
	db *sql.DB
}

// NewPostgresInspector creates a new PostgreSQL database inspector
func NewPostgresInspector(dsn string) (*PostgresInspector, error) {
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	return &PostgresInspector{db: db}, nil
}

// Close closes the database connection
func (i *PostgresInspector) Close() error {
	return i.db.Close()
}

// ListDatabases returns all databases on the PostgreSQL server
func (i *PostgresInspector) ListDatabases() ([]Database, error) {
	query := `
		SELECT 
			datname, 
			pg_encoding_to_char(encoding), 
			datcollate 
		FROM pg_database 
		WHERE datistemplate = false
		ORDER BY datname
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
func (i *PostgresInspector) ListTables(database string) ([]Table, error) {
	query := `
		SELECT 
			t.tablename,
			'heap' AS engine,
			COALESCE(c.reltuples, 0)::bigint AS rows,
			COALESCE(pg_total_relation_size(t.schemaname||'.'||t.tablename), 0)::bigint AS total_size
		FROM pg_tables t
		LEFT JOIN pg_class c ON c.relname = t.tablename 
			AND c.relnamespace = (SELECT oid FROM pg_namespace WHERE nspname = t.schemaname)
		WHERE t.schemaname = $1
		ORDER BY t.tablename
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
		t.IndexSize = 0 // PostgreSQL combines this
		tables = append(tables, t)
	}

	return tables, rows.Err()
}

// GetTableDetails returns detailed information about a specific table
func (i *PostgresInspector) GetTableDetails(database, table string) (map[string]interface{}, error) {
	query := `
		SELECT 
			c.relname,
			COALESCE(c.reltuples, 0)::bigint AS rows,
			COALESCE(pg_total_relation_size(c.oid), 0)::bigint AS total_size,
			COALESCE(pg_relation_size(c.oid), 0)::bigint AS data_size,
			COALESCE(pg_indexes_size(c.oid), 0)::bigint AS index_size,
			pg_get_userbyid(c.relowner) AS owner,
			COALESCE(obj_description(c.oid, 'pg_class'), '') AS description
		FROM pg_class c
		JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = $1 AND c.relname = $2 AND c.relkind = 'r'
	`

	details := make(map[string]interface{})

	var name, owner, description string
	var rows, totalSize, dataSize, indexSize int64

	err := i.db.QueryRow(query, database, table).Scan(
		&name, &rows, &totalSize, &dataSize, &indexSize, &owner, &description,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to get table details: %w", err)
	}

	details["name"] = name
	details["engine"] = "heap (PostgreSQL)"
	details["rows"] = rows
	details["total_size"] = totalSize
	details["data_size"] = dataSize
	details["index_size"] = indexSize
	details["owner"] = owner
	details["description"] = description

	return details, nil
}
