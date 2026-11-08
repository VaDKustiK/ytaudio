package config

import (
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
	for _, f := range []string{"mp3", "wav", "flac", "mp4", "ogg"} {
		c := Defaults()
		c.Format = f
		c.OutputFolder = filepath.Clean(t.TempDir())
		if err := c.Validate(); err != nil {
			t.Fatal(err)
		}
	}
}
