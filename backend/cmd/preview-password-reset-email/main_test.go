package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPreviewCapturesProductionMarkupWithOnlyCIDResolution(t *testing.T) {
	dir := t.TempDir()
	if err := writePreview(dir, 7*time.Minute+time.Second); err != nil {
		t.Fatal(err)
	}
	read := func(name string) string {
		t.Helper()
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	production, preview := read("production.html"), read("preview.html")
	if strings.ReplaceAll(production, "cid:vellum-mark@vellum", "assets/vellum-mark.png") != preview || strings.Count(production, "8 minutes") != 2 || !strings.Contains(read("plain-text.txt"), "8 minutes") {
		t.Fatal("preview diverges from production sender or configured expiry")
	}
	if len(read("assets/vellum-mark.png")) == 0 || !strings.Contains(production, "https://vellum.example/reset-password#token=PREVIEW_ONLY_") {
		t.Fatal("missing captured asset or synthetic URL")
	}
}
