package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/db-stream/mysql-inspect/internal/config"
	"github.com/db-stream/mysql-inspect/internal/db"
)

func main() {
	driver := flag.String("driver", "mysql", "Database driver: mysql or postgres")
	databases := flag.Bool("databases", false, "List all databases")
	tables := flag.String("tables", "", "List tables in a specific database")
	details := flag.String("details", "", "Get details for a specific table (format: database.table)")
	pretty := flag.Bool("pretty", true, "Pretty print JSON output")
	flag.Parse()

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	var inspector db.Inspector

	switch *driver {
	case "mysql":
		inspector, err = db.NewMySQLInspector(cfg.DSN())
		if err != nil {
			log.Fatalf("Failed to connect to MySQL: %v", err)
		}
	case "postgres":
		dsn := cfg.PostgresDSN()
		inspector, err = db.NewPostgresInspector(dsn)
		if err != nil {
			log.Fatalf("Failed to connect to PostgreSQL: %v", err)
		}
	default:
		log.Fatalf("Unknown driver: %s (supported: mysql, postgres)", *driver)
	}
	defer inspector.Close()

	// List databases
	if *databases {
		dbs, err := inspector.ListDatabases()
		if err != nil {
			log.Fatalf("Failed to list databases: %v", err)
		}
		printOutput(dbs, *pretty)
		return
	}

	// List tables in a database
	if *tables != "" {
		tbls, err := inspector.ListTables(*tables)
		if err != nil {
			log.Fatalf("Failed to list tables: %v", err)
		}
		printOutput(tbls, *pretty)
		return
	}

	// Get table details
	if *details != "" {
		var dbName, tableName string
		if _, err := fmt.Sscanf(*details, "%s.%s", &dbName, &tableName); err != nil {
			log.Fatalf("Failed to parse details argument: %v", err)
		}

		details, err := inspector.GetTableDetails(dbName, tableName)
		if err != nil {
			log.Fatalf("Failed to get table details: %v", err)
		}
		printOutput(details, *pretty)
		return
	}

	// Default: show usage
	printUsage()
}

func printOutput(data interface{}, pretty bool) {
	var output []byte
	var err error

	if pretty {
		output, err = json.MarshalIndent(data, "", "  ")
	} else {
		output, err = json.Marshal(data)
	}

	if err != nil {
		log.Fatalf("Failed to marshal output: %v", err)
	}

	fmt.Println(string(output))
}

func printUsage() {
	fmt.Println("Database Inspector")
	fmt.Println("")
	fmt.Println("Usage:")
	fmt.Println("  db-inspect -driver mysql -databases              List all MySQL databases")
	fmt.Println("  db-inspect -driver postgres -databases           List all PostgreSQL databases")
	fmt.Println("  db-inspect -driver mysql -tables <database>       List tables in a MySQL database")
	fmt.Println("  db-inspect -driver postgres -tables <schema>      List tables in a PostgreSQL schema")
	fmt.Println("  db-inspect -driver mysql -details <db>.<table>   Get MySQL table details")
	fmt.Println("  db-inspect -driver postgres -details <schema>.<table> Get PostgreSQL table details")
	fmt.Println("")
	fmt.Println("Environment Variables (MySQL):")
	fmt.Println("  MYSQL_HOST     Database host (default: localhost)")
	fmt.Println("  MYSQL_PORT     Database port (default: 3307)")
	fmt.Println("  MYSQL_USER     Database user (default: root)")
	fmt.Println("  MYSQL_PASSWORD Database password (default: root_password)")
	fmt.Println("")
	fmt.Println("Environment Variables (PostgreSQL):")
	fmt.Println("  PG_HOST     Database host (default: localhost)")
	fmt.Println("  PG_PORT     Database port (default: 5433)")
	fmt.Println("  PG_USER     Database user (default: db_stream)")
	fmt.Println("  PG_PASSWORD Database password (default: db_stream_password)")
	os.Exit(1)
}
