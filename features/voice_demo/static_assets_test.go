package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStaticIndexHasLayerTestIDs(t *testing.T) {
	path := filepath.Join("static", "index.html")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	html := string(raw)
	needles := []string{
		`data-testid="status-realtime"`,
		`data-testid="status-cawa"`,
		`data-testid="bargein-mode"`,
		`data-testid="bargein-stop-label"`,
		`data-testid="vad-silence-ms"`,
		`data-testid="vad-threshold"`,
		`data-testid="vad-prefix-ms"`,
		`data-testid="vad-applied"`,
		"Applies on next Realtime connect",
		">disconnected<",
		">no session<",
		`value="natural" selected`,
	}
	for _, n := range needles {
		if !strings.Contains(html, n) {
			t.Fatalf("index.html missing %q", n)
		}
	}
}
