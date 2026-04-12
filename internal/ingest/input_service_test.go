package ingest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestIngestFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "notes.md")
	content := "# Decision\nShip parser first"
	if err := osWriteFile(path, []byte(content)); err != nil {
		t.Fatalf("write file: %v", err)
	}

	fixedNow := time.Date(2026, 4, 12, 10, 0, 0, 0, time.UTC)
	svc := Service{
		now: func() time.Time { return fixedNow },
		nextID: func() (string, error) {
			return "in_test", nil
		},
	}

	input, gotContent, err := svc.IngestFile(path)
	if err != nil {
		t.Fatalf("IngestFile() error = %v", err)
	}
	if input.ID != "in_test" {
		t.Fatalf("IngestFile() input.ID = %q, want in_test", input.ID)
	}
	if input.Type != "md" {
		t.Fatalf("IngestFile() input.Type = %q, want md", input.Type)
	}
	if input.Format != "md" {
		t.Fatalf("IngestFile() input.Format = %q, want md", input.Format)
	}
	if gotContent != content {
		t.Fatalf("IngestFile() content = %q, want %q", gotContent, content)
	}
	if input.CreatedAt != fixedNow {
		t.Fatalf("IngestFile() created_at = %v, want %v", input.CreatedAt, fixedNow)
	}
	if !strings.HasSuffix(input.Metadata["source_path"], "notes.md") {
		t.Fatalf("IngestFile() source_path = %q, expected notes.md suffix", input.Metadata["source_path"])
	}
}

func TestIngestFileRejectsUnsupportedExtension(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "notes.xml")
	if err := osWriteFile(path, []byte("<x/>")); err != nil {
		t.Fatalf("write file: %v", err)
	}

	svc := NewService()
	_, _, err := svc.IngestFile(path)
	if err == nil {
		t.Fatal("IngestFile() expected error for unsupported extension")
	}
}

func TestIngestText(t *testing.T) {
	svc := Service{
		now: func() time.Time {
			return time.Date(2026, 4, 12, 11, 0, 0, 0, time.UTC)
		},
		nextID: func() (string, error) {
			return "in_text", nil
		},
	}

	input, content, err := svc.IngestText("  user says metrics dropped  ", map[string]string{"channel": "cli"})
	if err != nil {
		t.Fatalf("IngestText() error = %v", err)
	}
	if input.Type != "text" {
		t.Fatalf("IngestText() type = %q, want text", input.Type)
	}
	if input.Format != "raw" {
		t.Fatalf("IngestText() format = %q, want raw", input.Format)
	}
	if content != "user says metrics dropped" {
		t.Fatalf("IngestText() content = %q, want trimmed text", content)
	}
	if input.Metadata["source"] != "raw_text" {
		t.Fatalf("IngestText() source metadata = %q, want raw_text", input.Metadata["source"])
	}
	if input.Metadata["channel"] != "cli" {
		t.Fatalf("IngestText() channel metadata = %q, want cli", input.Metadata["channel"])
	}
}

func osWriteFile(path string, data []byte) error {
	return os.WriteFile(path, data, 0o600)
}
