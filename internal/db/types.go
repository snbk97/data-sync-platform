package db

// Database represents a database schema
type Database struct {
	Name      string
	Charset   string
	Collation string
}

// Table represents a database table
type Table struct {
	Name      string
	Engine    string
	Rows      int64
	DataSize  int64
	IndexSize int64
}

// Inspector defines the interface for database inspection
type Inspector interface {
	// ListDatabases returns all databases on the server
	ListDatabases() ([]Database, error)

	// ListTables returns all tables in a given database
	ListTables(database string) ([]Table, error)

	// GetTableDetails returns detailed information about a specific table
	GetTableDetails(database, table string) (map[string]interface{}, error)

	// Close closes the database connection
	Close() error
}
