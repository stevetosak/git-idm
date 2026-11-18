package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stevetosak/git-idm/internal/agent"
)

func runAgent(t *testing.T, args ...string) (string, error) {
	t.Helper()
	root := newRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs(append([]string{"agent"}, args...))
	err := root.Execute()
	return out.String(), err
}

func TestAgentCommand(t *testing.T) {
	path := filepath.Join(t.TempDir(), "skills", "git-idm", "SKILL.md")
	old := skillPath
	t.Cleanup(func() { skillPath = old })
	skillPath = func() string { return path }

	if out, err := runAgent(t); err != nil || out != agent.Instructions() {
		t.Fatalf("agent: %v\n%s", err, out)
	}
	if out, err := runAgent(t, "install"); err != nil || !strings.Contains(out, "wrote ") {
		t.Fatalf("install: %v\n%s", err, out)
	}
	if data, _ := os.ReadFile(path); string(data) != agent.Skill() {
		t.Error("skill not written")
	}
	if out, err := runAgent(t, "install"); err != nil || !strings.Contains(out, "is up to date") {
		t.Fatalf("second install: %v\n%s", err, out)
	}
	if out, err := runAgent(t, "remove"); err != nil || !strings.Contains(out, "removed ") {
		t.Fatalf("remove: %v\n%s", err, out)
	}
	if _, err := runAgent(t, "unknown"); err == nil {
		t.Error("an unknown argument must fail")
	}
}
