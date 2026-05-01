package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/felixgeelhaar/mnemos/internal/domain"
	"github.com/felixgeelhaar/mnemos/internal/markdown"
)

// handleExport routes `mnemos export --kind=lesson|playbook [--id=ID]`.
// Default writes to stdout; --out <path> writes to a file.
func handleExport(args []string, _ Flags) {
	var kind, id, out string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--kind":
			kind = args[i+1]
			i++
		case "--id":
			id = args[i+1]
			i++
		case "--out":
			out = args[i+1]
			i++
		default:
			exitWithMnemosError(false, NewUserError("unknown flag %q", args[i]))
			return
		}
	}
	if strings.TrimSpace(kind) == "" || strings.TrimSpace(id) == "" {
		exitWithMnemosError(false, NewUserError("--kind and --id are required"))
		return
	}
	ctx := context.Background()
	conn, err := openConn(ctx)
	if err != nil {
		exitWithMnemosError(false, NewSystemError(err, "open store"))
		return
	}
	defer closeConn(conn)
	var rendered string
	switch kind {
	case "lesson":
		l, err := conn.Lessons.GetByID(ctx, id)
		if err != nil {
			exitWithMnemosError(false, NewSystemError(err, "get lesson"))
			return
		}
		rendered, err = markdown.ExportLesson(l)
		if err != nil {
			exitWithMnemosError(false, NewSystemError(err, "render markdown"))
			return
		}
	case "playbook":
		p, err := conn.Playbooks.GetByID(ctx, id)
		if err != nil {
			exitWithMnemosError(false, NewSystemError(err, "get playbook"))
			return
		}
		rendered, err = markdown.ExportPlaybook(p)
		if err != nil {
			exitWithMnemosError(false, NewSystemError(err, "render markdown"))
			return
		}
	default:
		exitWithMnemosError(false, NewUserError("unknown kind %q (want lesson | playbook)", kind))
		return
	}
	if out == "" {
		fmt.Print(rendered)
		return
	}
	if err := os.WriteFile(out, []byte(rendered), 0o600); err != nil { //nolint:gosec // G304: --out path is the operator's choice
		exitWithMnemosError(false, NewSystemError(err, "write file"))
		return
	}
	emitJSON(map[string]string{"path": out, "kind": kind, "id": id})
}

// handleImport reads a markdown file and upserts it as the matching
// entity. Source defaults to "human" when the file has no source
// field — Mnemos treats hand-authored content as human-source so the
// trust formula can weight it differently from synthesised content.
func handleImport(args []string, _ Flags) {
	if len(args) == 0 {
		exitWithMnemosError(false, NewUserError("import requires a path"))
		return
	}
	path := args[0]
	data, err := os.ReadFile(path) //nolint:gosec // G304: caller-supplied path is the operator's choice
	if err != nil {
		exitWithMnemosError(false, NewSystemError(err, "read file"))
		return
	}
	doc, err := markdown.Parse(string(data))
	if err != nil {
		exitWithMnemosError(false, NewUserError("parse markdown: %v", err))
		return
	}
	ctx := context.Background()
	conn, err := openConn(ctx)
	if err != nil {
		exitWithMnemosError(false, NewSystemError(err, "open store"))
		return
	}
	defer closeConn(conn)
	switch doc.Kind {
	case "lesson":
		l := *doc.Lesson
		ensureMarkdownDefaults(&l, nil)
		if err := conn.Lessons.Append(ctx, l); err != nil {
			exitWithMnemosError(false, NewSystemError(err, "append lesson"))
			return
		}
		emitJSON(map[string]string{"kind": "lesson", "id": l.ID, "source": l.Source})
	case "playbook":
		p := *doc.Playbook
		ensureMarkdownDefaults(nil, &p)
		if err := conn.Playbooks.Append(ctx, p); err != nil {
			exitWithMnemosError(false, NewSystemError(err, "append playbook"))
			return
		}
		emitJSON(map[string]string{"kind": "playbook", "id": p.ID, "source": p.Source})
	default:
		exitWithMnemosError(false, NewUserError("unknown markdown kind %q", doc.Kind))
	}
}

// ensureMarkdownDefaults backfills the fields a hand-authored
// markdown file may legitimately omit. Confidence defaults to 0.6
// (above the synthesis floor of 0.55 so a hand-authored entry
// surfaces in queries) and timestamps default to now.
func ensureMarkdownDefaults(l *domain.Lesson, p *domain.Playbook) {
	now := time.Now().UTC()
	if l != nil {
		if l.Source == "" {
			l.Source = "human"
		}
		if l.Confidence == 0 {
			l.Confidence = 0.6
		}
		if l.DerivedAt.IsZero() {
			l.DerivedAt = now
		}
		if len(l.Evidence) == 0 {
			// Validate requires at least one evidence id; humans who
			// hand-author lessons rarely cite raw action ids, so we
			// accept "human" as a placeholder marking provenance.
			l.Evidence = []string{"human"}
		}
	}
	if p != nil {
		if p.Source == "" {
			p.Source = "human"
		}
		if p.Confidence == 0 {
			p.Confidence = 0.6
		}
		if p.DerivedAt.IsZero() {
			p.DerivedAt = now
		}
	}
}
