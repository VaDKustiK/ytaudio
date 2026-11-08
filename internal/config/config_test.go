package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func isolate(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("APPDATA", dir)
	t.Setenv("YTAUDIO_CONFIG_DIR", dir)
}
func TestConfigRoundTripAndCorruption(t *testing.T) {
	isolate(t)
	cfg := Defaults()
	cfg.OutputFolder = t.TempDir()
	cfg.Format = "ogg"
	cfg.Proxy = "socks5://127.0.0.1:1080"
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
	got, err := Load()
	if err != nil || got != cfg {
		t.Fatalf("%#v %v", got, err)
	}
	path, _ := filePath()
	os.WriteFile(path, []byte("{broken"), 0600)
	got, err = Load()
	if err == nil || got != Defaults() {
		t.Fatalf("corruption did not fall back: %#v %v", got, err)
	}
}
func TestConfigValidation(t *testing.T) {
	for _, value := range []string{"relative", ""} {
		c := Defaults()
		c.OutputFolder = value
		if c.Validate() == nil {
			t.Fatal(value)
		}
	}
	for _, proxy := range []string{"ftp://host", "socks5://", "not-a-url"} {
		c := Defaults()
		c.Proxy = proxy
		if c.Validate() == nil {
			t.Fatal(proxy)
		}
	}
	for _, f := range []string{"mp3", "wav", "flac", "ogg"} {
		c := Defaults()
		c.Format = f
		c.OutputFolder = filepath.Clean(t.TempDir())
		if err := c.Validate(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestLoadMigratesRemovedMP4WithoutLosingPreferences(t *testing.T) {
	isolate(t)
	cfg := Defaults()
	cfg.Format = "mp4"
	cfg.OutputFolder = t.TempDir()
	cfg.Proxy = "http://127.0.0.1:8080"
	if cfg.Validate() == nil {
		t.Fatal("removed format accepted")
	}
	path, _ := filePath()
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	got, err := Load()
	cfg.Format = "mp3"
	if err != nil || got != cfg {
		t.Fatalf("migration lost preferences: %+v %v", got, err)
	}
}
