package ghaw

import (
	"os"
	"reflect"
	"testing"
)

const plainMD = `---
description: "AI-assisted daily grooming"
on:
  schedule: daily
  workflow_dispatch:
  reaction: eyes
engine: copilot
timeout-minutes: 30
safe-outputs:
  add-comment:
    max: 100
  add-labels:
  threat-detection: false
---

# Body

---
not frontmatter: true
---
`

const indentedMD = `  ---
  description: |
    Multi-line description.
    Second line.

  on:
    schedule:
      - cron: "36 11 * * 1-5"
      - cron: "0 0 * * 0"
    workflow_dispatch:
      inputs:
        target:
          description: Target
          required: true
    slash_command:
      name: test-assist
    reaction: "eyes"

  engine:
    id: claude
    model: claude-sonnet-5
  timeout-minutes: 20
  ---

Body text.
`

const lockWithInputs = `# comment
name: "Daily Test Improver"
on:
  schedule:
    - cron: "36 11 * * 1-5"
  workflow_dispatch:
    inputs:
      aw_context:
        default: ""
        description: "Agent caller context (used internally by Agentic Workflows)."
        required: false
        type: string
      target:
        description: Target
        required: true
        type: choice
        default: main
        options: [main, release]
      dry_run:
        type: boolean
        default: false
jobs: {}
`

func TestExtractFrontmatterIndented(t *testing.T) {
	fm, err := ExtractFrontmatter(indentedMD)
	if err != nil {
		t.Fatal(err)
	}
	if fm[:12] != "description:" {
		t.Fatalf("frontmatter not dedented: %q", fm[:20])
	}
}

func TestExtractFrontmatterMissing(t *testing.T) {
	if _, err := ExtractFrontmatter("# Just markdown\n"); err == nil {
		t.Fatal("expected error")
	}
	if _, err := ExtractFrontmatter("---\nfoo: bar\n"); err == nil {
		t.Fatal("expected error for unterminated frontmatter")
	}
}

func TestParsePlain(t *testing.T) {
	w, err := Parse([]byte(plainMD), []byte(lockWithInputs))
	if err != nil {
		t.Fatal(err)
	}
	if w.Description != "AI-assisted daily grooming" {
		t.Errorf("description = %q", w.Description)
	}
	if w.Schedule != "daily" {
		t.Errorf("schedule = %q", w.Schedule)
	}
	if !reflect.DeepEqual(w.Triggers, []string{"schedule", "workflow_dispatch"}) {
		t.Errorf("triggers = %v (reaction must be ignored)", w.Triggers)
	}
	if !w.Dispatchable {
		t.Error("workflow_dispatch: null must be dispatchable")
	}
	if w.Engine != "copilot" || w.Model != "" {
		t.Errorf("engine = %q model = %q", w.Engine, w.Model)
	}
	if w.TimeoutMinutes != 30 {
		t.Errorf("timeout = %d", w.TimeoutMinutes)
	}
	if !reflect.DeepEqual(w.SafeOutputs, []string{"add-comment", "add-labels"}) {
		t.Errorf("safeOutputs = %v", w.SafeOutputs)
	}
	if w.DisplayName != "Daily Test Improver" {
		t.Errorf("displayName = %q", w.DisplayName)
	}
	names := []string{}
	for _, in := range w.DispatchInputs {
		names = append(names, in.Name)
		if in.Name == "aw_context" {
			t.Error("aw_context must be excluded")
		}
	}
	if !reflect.DeepEqual(names, []string{"dry_run", "target"}) {
		t.Errorf("inputs = %v", names)
	}
	target := w.DispatchInputs[1]
	if !target.Required || target.Type != "choice" || target.Default != "main" || !reflect.DeepEqual(target.Options, []string{"main", "release"}) {
		t.Errorf("target input = %+v", target)
	}
	if w.DispatchInputs[0].Default != "false" {
		t.Errorf("boolean default = %q", w.DispatchInputs[0].Default)
	}
}

func TestParseIndented(t *testing.T) {
	w, err := Parse([]byte(indentedMD), nil)
	if err != nil {
		t.Fatal(err)
	}
	if w.Description != "Multi-line description.\nSecond line." {
		t.Errorf("description = %q", w.Description)
	}
	if w.Schedule != "36 11 * * 1-5, 0 0 * * 0" {
		t.Errorf("schedule = %q", w.Schedule)
	}
	if w.Engine != "claude" || w.Model != "claude-sonnet-5" {
		t.Errorf("engine = %q model = %q", w.Engine, w.Model)
	}
	if w.SlashCommand != "test-assist" {
		t.Errorf("slashCommand = %q", w.SlashCommand)
	}
	if !reflect.DeepEqual(w.Triggers, []string{"schedule", "slash_command", "workflow_dispatch"}) {
		t.Errorf("triggers = %v", w.Triggers)
	}
	if !w.Dispatchable {
		t.Error("expected dispatchable")
	}
}

func TestParseNotDispatchableHasNoInputs(t *testing.T) {
	md := "---\non:\n  issues:\n    types: [opened]\n  reaction: eyes\n---\n"
	w, err := Parse([]byte(md), []byte(lockWithInputs))
	if err != nil {
		t.Fatal(err)
	}
	if w.Dispatchable || len(w.DispatchInputs) != 0 {
		t.Errorf("dispatchable = %v inputs = %v", w.Dispatchable, w.DispatchInputs)
	}
	if !reflect.DeepEqual(w.Triggers, []string{"issues"}) {
		t.Errorf("triggers = %v", w.Triggers)
	}
}

func TestParseRealFixtures(t *testing.T) {
	cases := []struct {
		name         string
		dispatchable bool
		displayName  string
		schedule     string
	}{
		{"daily-test-improver", true, "Daily Test Improver", "36 11 * * 1-5"},
		{"issue-triage", false, "Agentic Triage", ""},
	}
	for _, c := range cases {
		md, err := os.ReadFile("testdata/" + c.name + ".md")
		if err != nil {
			t.Fatal(err)
		}
		lock, err := os.ReadFile("testdata/" + c.name + ".lock.yml")
		if err != nil {
			t.Fatal(err)
		}
		w, err := Parse(md, lock)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if w.Dispatchable != c.dispatchable {
			t.Errorf("%s dispatchable = %v", c.name, w.Dispatchable)
		}
		if w.DisplayName != c.displayName {
			t.Errorf("%s displayName = %q", c.name, w.DisplayName)
		}
		if w.Schedule != c.schedule {
			t.Errorf("%s schedule = %q", c.name, w.Schedule)
		}
		if len(w.DispatchInputs) != 0 {
			t.Errorf("%s inputs = %v (aw_context must be excluded)", c.name, w.DispatchInputs)
		}
		if w.Description == "" || len(w.SafeOutputs) == 0 {
			t.Errorf("%s: description/safeOutputs missing: %+v", c.name, w)
		}
	}
}
