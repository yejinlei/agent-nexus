package cmd

import (
	"bufio"
	"strings"
	"testing"
)

func TestPromptModelSelection_NilReader(t *testing.T) {
	models := []string{"gpt-5.5", "claude-sonnet-5"}
	model, action := PromptModelSelection("codex", "live", models, nil)
	if model != "" {
		t.Fatalf("expected empty model, got %q", model)
	}
	if action != ActionAuto {
		t.Fatalf("expected ActionAuto, got %v", action)
	}
}

func TestPromptModelSelection_SingleModel(t *testing.T) {
	reader := bufio.NewReader(strings.NewReader("1\n"))
	models := []string{"gpt-5.5"}
	model, action := PromptModelSelection("codex", "live", models, reader)
	if model != "" {
		t.Fatalf("expected empty model, got %q", model)
	}
	if action != ActionAuto {
		t.Fatalf("expected ActionAuto, got %v", action)
	}
}

func TestPromptModelSelection_EnterAuto(t *testing.T) {
	reader := bufio.NewReader(strings.NewReader("\n"))
	models := []string{"deepseek-v4-flash", "gpt-5.5", "gpt-5.4"}
	model, action := PromptModelSelection("codex", "live", models, reader)
	if action != ActionAuto {
		t.Fatalf("expected ActionAuto, got %v", action)
	}
	// codex defaults to gpt-5.5 → keyword match
	if model != "gpt-5.5" {
		t.Fatalf("expected gpt-5.5, got %q", model)
	}
}

func TestPromptModelSelection_NumberPick(t *testing.T) {
	reader := bufio.NewReader(strings.NewReader("3\n"))
	models := []string{"deepseek-v4-flash", "gpt-5.5", "glm-5.2"}
	model, action := PromptModelSelection("codex", "live", models, reader)
	if action != ActionAuto {
		t.Fatalf("expected ActionAuto, got %v", action)
	}
	if model != "glm-5.2" {
		t.Fatalf("expected glm-5.2, got %q", model)
	}
}

func TestPromptModelSelection_Skip(t *testing.T) {
	reader := bufio.NewReader(strings.NewReader("s\n"))
	models := []string{"gpt-5.5", "gpt-5.4"}
	model, action := PromptModelSelection("codex", "live", models, reader)
	if model != "" {
		t.Fatalf("expected empty model, got %q", model)
	}
	if action != ActionSkip {
		t.Fatalf("expected ActionSkip, got %v", action)
	}
}

func TestPromptModelSelection_AcceptAll(t *testing.T) {
	reader := bufio.NewReader(strings.NewReader("a\n"))
	models := []string{"gpt-5.5", "gpt-5.4"}
	model, action := PromptModelSelection("codex", "live", models, reader)
	if action != ActionAcceptAll {
		t.Fatalf("expected ActionAcceptAll, got %v", action)
	}
	if model != "gpt-5.5" {
		t.Fatalf("expected gpt-5.5, got %q", model)
	}
}

func TestPromptModelSelection_Quit(t *testing.T) {
	reader := bufio.NewReader(strings.NewReader("q\n"))
	models := []string{"gpt-5.5", "gpt-5.4"}
	model, action := PromptModelSelection("codex", "live", models, reader)
	if model != "" {
		t.Fatalf("expected empty model, got %q", model)
	}
	if action != ActionQuit {
		t.Fatalf("expected ActionQuit, got %v", action)
	}
}

func TestPromptModelSelection_InvalidFallsBack(t *testing.T) {
	reader := bufio.NewReader(strings.NewReader("hello\n"))
	models := []string{"gpt-5.5", "gpt-5.4"}
	model, action := PromptModelSelection("codex", "live", models, reader)
	if action != ActionAuto {
		t.Fatalf("expected ActionAuto, got %v", action)
	}
	if model != "gpt-5.5" {
		t.Fatalf("expected fallback to recommended gpt-5.5, got %q", model)
	}
}

func TestPromptModelSelection_OutOfRangeFallsBack(t *testing.T) {
	reader := bufio.NewReader(strings.NewReader("99\n"))
	models := []string{"gpt-5.5", "gpt-5.4"}
	model, action := PromptModelSelection("codex", "live", models, reader)
	if action != ActionAuto {
		t.Fatalf("expected ActionAuto, got %v", action)
	}
	if model != "gpt-5.5" {
		t.Fatalf("expected fallback to recommended gpt-5.5, got %q", model)
	}
}

func TestPromptModelSelection_UseRecommendedModelInAuto(t *testing.T) {
	reader := bufio.NewReader(strings.NewReader("\n"))
	models := []string{"deepseek-v4-flash", "gpt-5.5"}
	model, _ := PromptModelSelection("codex", "live", models, reader)
	if model != "gpt-5.5" {
		t.Fatalf("expected recommended gpt-5.5 to be used when action is auto, got %q", model)
	}
}