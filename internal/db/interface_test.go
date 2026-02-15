package db

import "testing"

// TestInterfaceCompliance ensures implementations satisfy the Inspector interface
func TestInterfaceCompliance(t *testing.T) {
	var _ Inspector = (*MySQLInspector)(nil)
	var _ Inspector = (*PostgresInspector)(nil)
}
