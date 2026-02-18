package main

import "testing"

func TestParseTableName(t *testing.T) {
	tests := []struct {
		name    string
		tableID string
		want    string
	}{
		{name: "quoted mysql id", tableID: `"employee"."department"`, want: "department"},
		{name: "backtick id", tableID: "`employee`.`salary`", want: "salary"},
		{name: "plain id", tableID: "employee.dept_emp", want: "dept_emp"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseTableName(tt.tableID)
			if got != tt.want {
				t.Fatalf("parseTableName() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestResolveTargetDatabase(t *testing.T) {
	mapping := map[string]string{
		"mysql_employee_mirror": "mysql_employee_mirror",
		"pg_orion":              "pg_orion_mirror",
	}

	if got := resolveTargetDatabase("pg_orion", mapping); got != "pg_orion_mirror" {
		t.Fatalf("resolveTargetDatabase() = %q, want %q", got, "pg_orion_mirror")
	}

	if got := resolveTargetDatabase("mysql_employee_mirror", mapping); got != "mysql_employee_mirror" {
		t.Fatalf("resolveTargetDatabase() = %q, want %q", got, "mysql_employee_mirror")
	}

	if got := resolveTargetDatabase("unknown_source", mapping); got != "unknown_source" {
		t.Fatalf("resolveTargetDatabase() = %q, want %q", got, "unknown_source")
	}
}

func TestMapSchemaColumnType(t *testing.T) {
	tests := []struct {
		name      string
		column    tableColumn
		wantType  string
		wantError bool
	}{
		{
			name: "boolean nullable",
			column: tableColumn{
				TypeName: "BOOLEAN",
				Optional: true,
			},
			wantType: "Nullable(UInt8)",
		},
		{
			name: "tinyint one maps bool",
			column: tableColumn{
				TypeName:       "TINYINT",
				TypeExpression: "tinyint(1)",
				Optional:       true,
			},
			wantType: "Nullable(UInt8)",
		},
		{
			name: "unsigned bigint",
			column: tableColumn{
				TypeName:       "BIGINT",
				TypeExpression: "BIGINT UNSIGNED",
			},
			wantType: "UInt64",
		},
		{
			name: "decimal precision scale",
			column: tableColumn{
				TypeName:       "DECIMAL",
				TypeExpression: "DECIMAL(12,4)",
			},
			wantType: "Decimal(12,4)",
		},
		{
			name: "datetime with precision",
			column: tableColumn{
				TypeName:       "DATETIME",
				TypeExpression: "datetime(6)",
			},
			wantType: "DateTime64(6)",
		},
		{
			name: "unsupported fallback",
			column: tableColumn{
				TypeName: "GEOMETRY",
				Optional: true,
			},
			wantType:  "Nullable(String)",
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotType, err := mapSchemaColumnType(tt.column)
			if gotType != tt.wantType {
				t.Fatalf("mapSchemaColumnType() type = %q, want %q", gotType, tt.wantType)
			}

			if tt.wantError && err == nil {
				t.Fatalf("mapSchemaColumnType() error = nil, want non-nil")
			}
			if !tt.wantError && err != nil {
				t.Fatalf("mapSchemaColumnType() error = %v, want nil", err)
			}
		})
	}
}
