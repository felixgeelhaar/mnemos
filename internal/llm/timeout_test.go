package llm

import (
	"testing"
	"time"
)

func TestTimeoutDefault(t *testing.T) {
	t.Setenv("MNEMOS_LLM_TIMEOUT", "")
	if got := Timeout(); got != defaultLLMTimeout {
		t.Fatalf("default timeout = %s, want %s", got, defaultLLMTimeout)
	}
}

func TestTimeoutOverride(t *testing.T) {
	t.Setenv("MNEMOS_LLM_TIMEOUT", "45s")
	if got := Timeout(); got != 45*time.Second {
		t.Fatalf("override = %s, want 45s", got)
	}
}

func TestTimeoutInvalidFallsBack(t *testing.T) {
	t.Setenv("MNEMOS_LLM_TIMEOUT", "not-a-duration")
	if got := Timeout(); got != defaultLLMTimeout {
		t.Fatalf("invalid value should fall back to default, got %s", got)
	}
}

func TestTimeoutNegativeFallsBack(t *testing.T) {
	t.Setenv("MNEMOS_LLM_TIMEOUT", "-5s")
	if got := Timeout(); got != defaultLLMTimeout {
		t.Fatalf("negative duration should fall back, got %s", got)
	}
}
