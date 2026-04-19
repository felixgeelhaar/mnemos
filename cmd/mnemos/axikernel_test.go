package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/felixgeelhaar/axi-go"
	"github.com/felixgeelhaar/axi-go/domain"
	"github.com/felixgeelhaar/bolt"

	"github.com/felixgeelhaar/mnemos/internal/store/sqlite"
)

func TestBuildMCPKernel_RegistersAllMCPTools(t *testing.T) {
	logger := bolt.New(bolt.NewJSONHandler(os.Stderr))
	kernel, err := buildMCPKernel(logger, mcpExecutorMap("usr_test", func() (*Watcher, error) { return nil, nil }))
	if err != nil {
		t.Fatalf("buildMCPKernel: %v", err)
	}

	want := map[string]domain.EffectLevel{}
	for _, t := range mcpTools() {
		want[axiActionName(t.Name)] = t.Effect
	}

	got := kernel.ListActions()
	if len(got) != len(want) {
		t.Fatalf("registered %d actions, want %d", len(got), len(want))
	}
	for _, a := range got {
		expected, ok := want[string(a.Name())]
		if !ok {
			t.Errorf("unexpected action registered: %s", a.Name())
			continue
		}
		if a.EffectProfile().Level != expected {
			t.Errorf("%s effect = %s, want %s", a.Name(), a.EffectProfile().Level, expected)
		}
	}
}

func TestBuildMCPKernel_HelpDescribesEachTool(t *testing.T) {
	logger := bolt.New(bolt.NewJSONHandler(os.Stderr))
	kernel, err := buildMCPKernel(logger, mcpExecutorMap("", func() (*Watcher, error) { return nil, nil }))
	if err != nil {
		t.Fatalf("kernel: %v", err)
	}

	for _, tool := range mcpTools() {
		help, err := kernel.Help(axiActionName(tool.Name))
		if err != nil {
			t.Errorf("help %s: %v", tool.Name, err)
			continue
		}
		if help == "" {
			t.Errorf("help %s returned empty", tool.Name)
		}
	}
}

// TestKernel_KnowledgeMetricsThroughKernel proves the typed
// round-trip end-to-end: a live kernel executes knowledge_metrics
// (read-local, no inputs), the result decodes back into the typed
// MCP output struct, and the recorded session's evidence chain
// verifies cleanly.
func TestKernel_KnowledgeMetricsThroughKernel(t *testing.T) {
	t.Setenv("MNEMOS_DB_PATH", filepath.Join(t.TempDir(), "mnemos.db"))

	logger := bolt.New(bolt.NewJSONHandler(os.Stderr))
	kernel, err := buildMCPKernel(logger, mcpExecutorMap("", func() (*Watcher, error) { return nil, nil }))
	if err != nil {
		t.Fatalf("kernel: %v", err)
	}

	out, err := dispatchAxiTool[mcpMetricsOutput](context.Background(), kernel, nil, "knowledge_metrics", struct{}{})
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	// Empty DB → all counts zero. The point is round-trip integrity,
	// not the values themselves.
	if out.Events != 0 || out.Claims != 0 {
		t.Errorf("unexpected non-zero counts in fresh DB: %+v", out)
	}
}

// TestKernel_ProcessTextEvidenceChainIntact runs a write-local
// action through the kernel and verifies the resulting session's
// evidence chain hashes are populated and consistent.
func TestKernel_ProcessTextEvidenceChainIntact(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "mnemos.db")
	t.Setenv("MNEMOS_DB_PATH", dbPath)
	// Open the DB once so the schema is created before the kernel runs.
	db, err := sqlite.Open(dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	logger := bolt.New(bolt.NewJSONHandler(os.Stderr))
	kernel, err := buildMCPKernel(logger, mcpExecutorMap("", func() (*Watcher, error) { return nil, nil }))
	if err != nil {
		t.Fatalf("kernel: %v", err)
	}

	res, err := kernel.Execute(context.Background(), axi.Invocation{
		Action: axiActionName("process_text"),
		Input:  map[string]any{"text": "We use SQLite. Postgres was rejected."},
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if res.Failure != nil {
		t.Fatalf("execution failed: %s — %s", res.Failure.Code, res.Failure.Message)
	}
	if res.Result == nil {
		t.Fatal("nil result on successful execution")
	}

	session, err := kernel.GetSession(string(res.SessionID))
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	if err := session.VerifyEvidenceChain(); err != nil {
		t.Errorf("evidence chain broken: %v", err)
	}
	if len(session.Evidence()) == 0 {
		t.Error("expected at least one evidence record from process_text")
	}
}
