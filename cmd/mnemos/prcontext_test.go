package main

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

func TestPRRecord_ParsesGhJsonShape(t *testing.T) {
	// Sample output from `gh pr list --state merged --json ...`.
	raw := []byte(`[{
		"author": {"login": "felix", "name": "Felix Geelhaar"},
		"body": "## Summary\n- Adds a feature\n- Fixes a bug",
		"mergedAt": "2026-04-12T14:17:55Z",
		"number": 42,
		"title": "feat: do the thing"
	}]`)

	var prs []prRecord
	if err := json.Unmarshal(raw, &prs); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(prs) != 1 {
		t.Fatalf("got %d prs, want 1", len(prs))
	}
	p := prs[0]
	if p.Number != 42 {
		t.Errorf("number = %d, want 42", p.Number)
	}
	if p.Title != "feat: do the thing" {
		t.Errorf("title = %q, want 'feat: do the thing'", p.Title)
	}
	if p.Author.Login != "felix" {
		t.Errorf("author.login = %q, want 'felix'", p.Author.Login)
	}
	wantTime := time.Date(2026, 4, 12, 14, 17, 55, 0, time.UTC)
	if !p.MergedAt.Equal(wantTime) {
		t.Errorf("mergedAt = %v, want %v", p.MergedAt, wantTime)
	}
}

func TestBuildPREvent_PopulatesMetadata(t *testing.T) {
	pr := prRecord{
		Number:   7,
		Title:    "fix: patch the leak",
		Body:     "Closes a memory leak in the worker loop.",
		MergedAt: time.Date(2026, 4, 15, 9, 0, 0, 0, time.UTC),
	}
	pr.Author.Login = "felix"

	now := time.Date(2026, 4, 17, 18, 0, 0, 0, time.UTC)
	event := buildPREvent("run-test", pr, now)

	if event.ID != "ev_pr_7" {
		t.Errorf("event.ID = %q, want 'ev_pr_7'", event.ID)
	}
	if event.SchemaVersion != schemaVersionGitPR {
		t.Errorf("schema_version = %q, want %q", event.SchemaVersion, schemaVersionGitPR)
	}
	if event.Metadata["source"] != "github_pr" {
		t.Errorf("metadata.source = %q, want 'github_pr'", event.Metadata["source"])
	}
	if event.Metadata["github_pr_number"] != "7" {
		t.Errorf("metadata.github_pr_number = %q, want '7'", event.Metadata["github_pr_number"])
	}
	if event.Metadata["github_pr_author"] != "felix" {
		t.Errorf("metadata.github_pr_author = %q", event.Metadata["github_pr_author"])
	}
	if !event.IngestedAt.Equal(now) {
		t.Errorf("ingested_at = %v, want %v", event.IngestedAt, now)
	}
	// Content combines title + body.
	if event.Content != "fix: patch the leak\n\nCloses a memory leak in the worker loop." {
		t.Errorf("content = %q, unexpected shape", event.Content)
	}
}

func TestBuildPREvent_TitleOnlyWhenBodyEmpty(t *testing.T) {
	pr := prRecord{Number: 1, Title: "chore: bump deps", MergedAt: time.Now().UTC()}
	event := buildPREvent("r", pr, time.Now().UTC())
	if event.Content != "chore: bump deps" {
		t.Errorf("content = %q, want title-only", event.Content)
	}
}

func TestExistingGitPRNumbers_LoadsFromMetadata(t *testing.T) {
	db, _ := openTestStore(t)

	ctx := context.Background()
	if _, err := db.ExecContext(ctx,
		`INSERT INTO events (id, run_id, schema_version, content, source_input_id, timestamp, metadata_json, ingested_at) VALUES
		 ('e1', 'r', 'v', 'c', 'i1', '2026-04-17T00:00:00Z', '{"github_pr_number":"4"}',   '2026-04-17T00:00:00Z'),
		 ('e2', 'r', 'v', 'c', 'i2', '2026-04-17T00:00:00Z', '{"github_pr_number":"5"}',   '2026-04-17T00:00:00Z'),
		 ('e3', 'r', 'v', 'c', 'i3', '2026-04-17T00:00:00Z', '{"source":"raw_text"}',      '2026-04-17T00:00:00Z')`,
	); err != nil {
		t.Fatalf("insert: %v", err)
	}

	got, err := existingGitPRNumbers(ctx, db)
	if err != nil {
		t.Fatalf("existingGitPRNumbers: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d PR numbers, want 2 (%v)", len(got), got)
	}
	if _, ok := got["4"]; !ok {
		t.Errorf("missing PR 4")
	}
	if _, ok := got["5"]; !ok {
		t.Errorf("missing PR 5")
	}
}
