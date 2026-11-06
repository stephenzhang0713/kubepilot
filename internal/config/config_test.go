package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSaveLoadAndPermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings", "config.json")
	c := Defaults()
	c.ModelKey = "private-key"
	c.ModelName = "custom"
	c.Context = "prod"
	if err := c.Save(path); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("settings permissions: %o", info.Mode().Perm())
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got != c {
		t.Fatalf("settings did not round-trip: %#v", got)
	}
}
func TestValidation(t *testing.T) {
	tests := []struct {
		i     int
		value string
	}{{0, "invalid"}, {5, "30s"}, {1, ""}}
	for _, tt := range tests {
		c := Defaults()
		if err := c.Set(tt.i, tt.value); err == nil {
			t.Errorf("field %d accepted %q", tt.i, tt.value)
		}
	}
	c := Defaults()
	if err := c.Set(5, "5m"); err != nil {
		t.Fatal(err)
	}
}

func TestLoadOldSettingsIgnoresRemovedFeatures(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"model_name":"saved-model","prometheus_url":"http://old","database_path":"old.db","audit_path":"old.jsonl"}`), 0600); err != nil {
		t.Fatal(err)
	}
	c, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if c.ModelName != "saved-model" {
		t.Fatal("existing model settings were not retained")
	}
}
