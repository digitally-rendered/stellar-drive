package sql

import (
	"testing"
)

func TestNewDialect(t *testing.T) {
	tests := []struct {
		driverName  string
		wantType    string
		wantPh1     string
		wantPh2     string
		wantJSON    string
		wantAutoInc string
		wantUpsert  string
		wantQuote   string
	}{
		{
			driverName:  "postgres",
			wantType:    "postgres",
			wantPh1:     "$1",
			wantPh2:     "$2",
			wantJSON:    "JSONB",
			wantAutoInc: "BIGSERIAL PRIMARY KEY",
			wantUpsert:  "ON CONFLICT DO NOTHING",
			wantQuote:   `"my_table"`,
		},
		{
			driverName:  "pgx",
			wantType:    "postgres",
			wantPh1:     "$1",
			wantPh2:     "$2",
			wantJSON:    "JSONB",
			wantAutoInc: "BIGSERIAL PRIMARY KEY",
			wantUpsert:  "ON CONFLICT DO NOTHING",
			wantQuote:   `"my_table"`,
		},
		{
			driverName:  "sqlite3",
			wantType:    "sqlite",
			wantPh1:     "?",
			wantPh2:     "?",
			wantJSON:    "TEXT",
			wantAutoInc: "INTEGER PRIMARY KEY AUTOINCREMENT",
			wantUpsert:  "ON CONFLICT DO NOTHING",
			wantQuote:   `"my_table"`,
		},
		{
			driverName:  "sqlite",
			wantType:    "sqlite",
			wantPh1:     "?",
			wantPh2:     "?",
			wantJSON:    "TEXT",
			wantAutoInc: "INTEGER PRIMARY KEY AUTOINCREMENT",
			wantUpsert:  "ON CONFLICT DO NOTHING",
			wantQuote:   `"my_table"`,
		},
		{
			driverName:  "unknown",
			wantType:    "sqlite", // safe default
			wantPh1:     "?",
			wantPh2:     "?",
			wantJSON:    "TEXT",
			wantAutoInc: "INTEGER PRIMARY KEY AUTOINCREMENT",
			wantUpsert:  "ON CONFLICT DO NOTHING",
			wantQuote:   `"my_table"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.driverName, func(t *testing.T) {
			d := NewDialect(tt.driverName)
			if d == nil {
				t.Fatal("NewDialect returned nil")
			}

			if got := d.Placeholder(1); got != tt.wantPh1 {
				t.Errorf("Placeholder(1) = %q, want %q", got, tt.wantPh1)
			}
			if got := d.Placeholder(2); got != tt.wantPh2 {
				t.Errorf("Placeholder(2) = %q, want %q", got, tt.wantPh2)
			}
			if got := d.JSONColumn(); got != tt.wantJSON {
				t.Errorf("JSONColumn() = %q, want %q", got, tt.wantJSON)
			}
			if got := d.AutoIncrement(); got != tt.wantAutoInc {
				t.Errorf("AutoIncrement() = %q, want %q", got, tt.wantAutoInc)
			}
			if got := d.UpsertSuffix(); got != tt.wantUpsert {
				t.Errorf("UpsertSuffix() = %q, want %q", got, tt.wantUpsert)
			}
			if got := d.QuoteIdentifier("my_table"); got != tt.wantQuote {
				t.Errorf("QuoteIdentifier(%q) = %q, want %q", "my_table", got, tt.wantQuote)
			}
		})
	}
}

func TestPostgresDialect_QuoteIdentifier_EscapesDoubleQuotes(t *testing.T) {
	d := PostgresDialect{}
	input := `weird"name`
	want := `"weird""name"`
	if got := d.QuoteIdentifier(input); got != want {
		t.Errorf("QuoteIdentifier(%q) = %q, want %q", input, got, want)
	}
}

func TestSQLiteDialect_QuoteIdentifier_EscapesDoubleQuotes(t *testing.T) {
	d := SQLiteDialect{}
	input := `weird"name`
	want := `"weird""name"`
	if got := d.QuoteIdentifier(input); got != want {
		t.Errorf("QuoteIdentifier(%q) = %q, want %q", input, got, want)
	}
}

func TestPostgresDialect_JSONExtract(t *testing.T) {
	d := PostgresDialect{}
	tests := []struct {
		column string
		field  string
		want   string
	}{
		{"data", "name", "data->>'name'"},
		{"data", "nested_field", "data->>'nested_field'"},
	}
	for _, tt := range tests {
		got := d.JSONExtract(tt.column, tt.field)
		if got != tt.want {
			t.Errorf("JSONExtract(%q, %q) = %q, want %q", tt.column, tt.field, got, tt.want)
		}
	}
}

func TestSQLiteDialect_JSONExtract(t *testing.T) {
	d := SQLiteDialect{}
	tests := []struct {
		column string
		field  string
		want   string
	}{
		{"data", "name", "json_extract(data, '$.name')"},
		{"data", "nested_field", "json_extract(data, '$.nested_field')"},
	}
	for _, tt := range tests {
		got := d.JSONExtract(tt.column, tt.field)
		if got != tt.want {
			t.Errorf("JSONExtract(%q, %q) = %q, want %q", tt.column, tt.field, got, tt.want)
		}
	}
}

func TestPostgresDialect_PlaceholderSequential(t *testing.T) {
	d := PostgresDialect{}
	for i := 1; i <= 5; i++ {
		want := "$" + string(rune('0'+i))
		got := d.Placeholder(i)
		if got != want {
			t.Errorf("Placeholder(%d) = %q, want %q", i, got, want)
		}
	}
}

func TestDialect_DriverName(t *testing.T) {
	tests := []struct {
		dialect    Dialect
		wantDriver string
	}{
		{PostgresDialect{}, "postgres"},
		{SQLiteDialect{}, "sqlite3"},
	}
	for _, tt := range tests {
		if got := tt.dialect.DriverName(); got != tt.wantDriver {
			t.Errorf("DriverName() = %q, want %q", got, tt.wantDriver)
		}
	}
}
