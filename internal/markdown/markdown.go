// Package markdown round-trips Mnemos lessons and playbooks to and
// from a Git-friendly markdown format. Frontmatter carries the
// structured fields (id, scope, confidence, etc.); the body carries
// the human-editable narrative (statement, evidence list, steps).
//
// The format is intentionally lossy on derived fields: confidence,
// last_verified, and source are written but ignored on import,
// because import re-runs the synthesis layer's contract — a
// hand-edited statement is treated as a "human" source until the next
// synthesis pass refreshes it.
package markdown

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/felixgeelhaar/mnemos/internal/domain"
)

// Document is the parsed shape of a markdown file. Kind discriminates
// between Lesson and Playbook; only the matching pointer is set.
type Document struct {
	Kind     string
	Lesson   *domain.Lesson
	Playbook *domain.Playbook
}

type frontmatter struct {
	ID                 string             `yaml:"id"`
	Kind               string             `yaml:"kind"` // "lesson" | "playbook"
	Trigger            string             `yaml:"trigger,omitempty"`
	ActionKind         string             `yaml:"action_kind,omitempty"` // domain.Lesson.Kind / domain.Playbook implied
	Scope              scopeYAML          `yaml:"scope,omitempty"`
	Confidence         float64            `yaml:"confidence,omitempty"`
	DerivedAt          string             `yaml:"derived_at,omitempty"`
	LastVerified       string             `yaml:"last_verified,omitempty"`
	Source             string             `yaml:"source,omitempty"`
	CreatedBy          string             `yaml:"created_by,omitempty"`
	Evidence           []string           `yaml:"evidence,omitempty"`
	DerivedFromLessons []string           `yaml:"derived_from_lessons,omitempty"`
	Steps              []playbookStepYAML `yaml:"steps,omitempty"`
}

type scopeYAML struct {
	Service string `yaml:"service,omitempty"`
	Env     string `yaml:"env,omitempty"`
	Team    string `yaml:"team,omitempty"`
}

type playbookStepYAML struct {
	Order       int    `yaml:"order"`
	Action      string `yaml:"action"`
	Description string `yaml:"description,omitempty"`
	Condition   string `yaml:"condition,omitempty"`
}

// ExportLesson renders a Lesson as YAML-frontmatter + markdown body.
// Returns the file content as a string. Stable: re-exporting the same
// lesson byte-for-byte returns the same string, so committing the
// output to git produces clean diffs.
func ExportLesson(l domain.Lesson) (string, error) {
	fm := frontmatter{
		ID:           l.ID,
		Kind:         "lesson",
		Trigger:      l.Trigger,
		ActionKind:   l.Kind,
		Scope:        scopeYAML{Service: l.Scope.Service, Env: l.Scope.Env, Team: l.Scope.Team},
		Confidence:   l.Confidence,
		DerivedAt:    formatTime(l.DerivedAt),
		LastVerified: formatTime(l.LastVerified),
		Source:       l.Source,
		CreatedBy:    l.CreatedBy,
		Evidence:     l.Evidence,
	}
	body := "# Statement\n\n" + l.Statement + "\n"
	return assemble(fm, body)
}

// ExportPlaybook renders a Playbook in the same shape.
func ExportPlaybook(p domain.Playbook) (string, error) {
	steps := make([]playbookStepYAML, 0, len(p.Steps))
	for _, s := range p.Steps {
		steps = append(steps, playbookStepYAML{
			Order:       s.Order,
			Action:      s.Action,
			Description: s.Description,
			Condition:   s.Condition,
		})
	}
	fm := frontmatter{
		ID:                 p.ID,
		Kind:               "playbook",
		Trigger:            p.Trigger,
		Scope:              scopeYAML{Service: p.Scope.Service, Env: p.Scope.Env, Team: p.Scope.Team},
		Confidence:         p.Confidence,
		DerivedAt:          formatTime(p.DerivedAt),
		LastVerified:       formatTime(p.LastVerified),
		Source:             p.Source,
		CreatedBy:          p.CreatedBy,
		DerivedFromLessons: p.DerivedFromLessons,
		Steps:              steps,
	}
	body := "# Statement\n\n" + p.Statement + "\n"
	return assemble(fm, body)
}

// Parse splits a markdown document into frontmatter + body and
// reconstructs either a Lesson or a Playbook depending on the
// frontmatter Kind. Body becomes Statement (the lesson/playbook
// narrative). Confidence/derived_at survive the round-trip but the
// import-side caller decides whether to honour them.
func Parse(content string) (Document, error) {
	fmText, body, err := splitFrontmatter(content)
	if err != nil {
		return Document{}, err
	}
	var fm frontmatter
	if err := yaml.Unmarshal([]byte(fmText), &fm); err != nil {
		return Document{}, fmt.Errorf("parse frontmatter: %w", err)
	}
	statement := extractStatement(body)
	switch fm.Kind {
	case "lesson":
		l := domain.Lesson{
			ID:           fm.ID,
			Statement:    statement,
			Trigger:      fm.Trigger,
			Kind:         fm.ActionKind,
			Scope:        domain.Scope{Service: fm.Scope.Service, Env: fm.Scope.Env, Team: fm.Scope.Team},
			Confidence:   fm.Confidence,
			DerivedAt:    parseTime(fm.DerivedAt),
			LastVerified: parseTime(fm.LastVerified),
			Source:       firstNonEmpty(fm.Source, "human"),
			CreatedBy:    fm.CreatedBy,
			Evidence:     fm.Evidence,
		}
		return Document{Kind: "lesson", Lesson: &l}, nil
	case "playbook":
		steps := make([]domain.PlaybookStep, 0, len(fm.Steps))
		for _, s := range fm.Steps {
			steps = append(steps, domain.PlaybookStep{
				Order:       s.Order,
				Action:      s.Action,
				Description: s.Description,
				Condition:   s.Condition,
			})
		}
		p := domain.Playbook{
			ID:                 fm.ID,
			Statement:          statement,
			Trigger:            fm.Trigger,
			Scope:              domain.Scope{Service: fm.Scope.Service, Env: fm.Scope.Env, Team: fm.Scope.Team},
			Steps:              steps,
			DerivedFromLessons: fm.DerivedFromLessons,
			Confidence:         fm.Confidence,
			DerivedAt:          parseTime(fm.DerivedAt),
			LastVerified:       parseTime(fm.LastVerified),
			Source:             firstNonEmpty(fm.Source, "human"),
			CreatedBy:          fm.CreatedBy,
		}
		return Document{Kind: "playbook", Playbook: &p}, nil
	default:
		return Document{}, fmt.Errorf("unknown markdown kind %q (want lesson | playbook)", fm.Kind)
	}
}

func assemble(fm frontmatter, body string) (string, error) {
	var b bytes.Buffer
	b.WriteString("---\n")
	enc := yaml.NewEncoder(&b)
	enc.SetIndent(2)
	if err := enc.Encode(fm); err != nil {
		return "", fmt.Errorf("encode frontmatter: %w", err)
	}
	if err := enc.Close(); err != nil {
		return "", fmt.Errorf("close yaml encoder: %w", err)
	}
	b.WriteString("---\n\n")
	b.WriteString(body)
	return b.String(), nil
}

func splitFrontmatter(content string) (string, string, error) {
	if !strings.HasPrefix(content, "---") {
		return "", "", errors.New("markdown must start with --- frontmatter")
	}
	rest := strings.TrimPrefix(content, "---\n")
	end := strings.Index(rest, "\n---\n")
	if end < 0 {
		end = strings.Index(rest, "\n---")
	}
	if end < 0 {
		return "", "", errors.New("frontmatter missing closing ---")
	}
	fm := rest[:end]
	body := strings.TrimPrefix(strings.TrimPrefix(rest[end:], "\n---\n"), "\n---")
	return fm, strings.TrimLeft(body, "\n"), nil
}

func extractStatement(body string) string {
	body = strings.TrimSpace(body)
	if !strings.HasPrefix(body, "# Statement") {
		return body
	}
	rest := strings.TrimPrefix(body, "# Statement")
	rest = strings.TrimLeft(rest, "\n")
	// Stop at the next H1 if the file embeds extra sections.
	if cut := strings.Index(rest, "\n# "); cut >= 0 {
		rest = rest[:cut]
	}
	return strings.TrimSpace(rest)
}

func formatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

func parseTime(s string) time.Time {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}
	}
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t
	}
	if t, err := time.Parse("2006-01-02", s); err == nil {
		return t
	}
	return time.Time{}
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
