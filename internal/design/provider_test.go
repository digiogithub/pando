package design

import (
	"database/sql"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

func TestNewProviderRejectsDatabasesMissingDesignTables(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	_, err = NewProvider(db)
	if err == nil {
		t.Fatal("NewProvider must fail when the design tables are missing")
	}
	for _, want := range []string{"database schema is outdated", "design tables are missing", "restart Pando"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("NewProvider error %q is missing %q", err, want)
		}
	}
}
