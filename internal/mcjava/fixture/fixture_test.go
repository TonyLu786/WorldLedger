package fixture

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadRejectsDuplicateJSONKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fixtures.json")
	data := `{"schema":"worldledger.minecraft.java.chunk-fixtures/v1","schema":"worldledger.minecraft.java.chunk-fixtures/v1","fixtures":[]}`
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "duplicate JSON key") {
		t.Fatalf("Load error = %v; want duplicate-key rejection", err)
	}
}

func TestLoadRejectsUnknownFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fixtures.json")
	data := `{"schema":"worldledger.minecraft.java.chunk-fixtures/v1","fixtures":[],"unexpected":true}`
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("Load error = %v; want unknown-field rejection", err)
	}
}
