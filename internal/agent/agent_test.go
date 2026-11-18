package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallRefreshRemove(t *testing.T) {
	path := filepath.Join(t.TempDir(), "skills", "git-idm", "SKILL.md")

	if changed, err := Refresh(path); err != nil || changed {
		t.Fatalf("refresh must not create the skill: %v %v", changed, err)
	}
	if changed, err := Install(path); err != nil || !changed {
		t.Fatalf("install: %v %v", changed, err)
	}
	if changed, err := Install(path); err != nil || changed {
		t.Fatalf("second install must change nothing: %v %v", changed, err)
	}

	os.WriteFile(path, []byte(marker+" old version -->\nold rules\n"), 0o644)
	if changed, err := Refresh(path); err != nil || !changed {
		t.Fatalf("refresh of an old skill: %v %v", changed, err)
	}
	if data, _ := os.ReadFile(path); string(data) != Skill() {
		t.Error("refresh must write the current skill")
	}

	if removed, err := Remove(path); err != nil || !removed {
		t.Fatalf("remove: %v %v", removed, err)
	}
	if _, err := os.Stat(filepath.Dir(path)); !os.IsNotExist(err) {
		t.Errorf("the empty skill folder must be removed: %v", err)
	}
}

func TestUserSkillIsNeverChanged(t *testing.T) {
	path := filepath.Join(t.TempDir(), "SKILL.md")
	own := "---\nname: git-idm\n---\nmy own rules\n"
	os.WriteFile(path, []byte(own), 0o644)

	if _, err := Install(path); err == nil {
		t.Error("install must refuse a skill that git-idm did not write")
	}
	if changed, _ := Refresh(path); changed {
		t.Error("refresh must not change a skill that git-idm did not write")
	}
	if removed, _ := Remove(path); removed {
		t.Error("remove must not delete a skill that git-idm did not write")
	}
	if data, _ := os.ReadFile(path); string(data) != own {
		t.Errorf("skill changed:\n%s", data)
	}
}

func TestSkillContent(t *testing.T) {
	if !strings.HasPrefix(Skill(), "---\nname: git-idm\ndescription: ") || !strings.Contains(Skill(), marker) {
		t.Errorf("the skill needs front matter and the marker:\n%s", Skill()[:200])
	}
	text := Instructions()
	if !strings.HasPrefix(text, "# git-idm\n") || strings.Contains(text, marker) || strings.Contains(text, "description:") {
		t.Errorf("instructions must have no front matter and no marker:\n%s", text[:200])
	}
	for _, rule := range []string{"--no-verify", "git-idm accept", "GIT_IDM_GH_CHECK=0", "Show the message and the fix line to the user"} {
		if !strings.Contains(text, rule) {
			t.Errorf("instructions miss %q", rule)
		}
	}
}

func TestDefaultPathFollowsClaudeConfigDir(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", "/c")
	if got := DefaultPath(); got != "/c/skills/git-idm/SKILL.md" {
		t.Errorf("DefaultPath = %s", got)
	}
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	t.Setenv("HOME", "/h")
	if got := DefaultPath(); got != "/h/.claude/skills/git-idm/SKILL.md" {
		t.Errorf("DefaultPath = %s", got)
	}
}
