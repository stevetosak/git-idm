package importer

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/stevetosak/git-idm/internal/config"
)

func TestLocalLegacy(t *testing.T) {
	home := sandboxHome(t)
	projects := filepath.Join(home, "Projects")
	repo(t, filepath.Join(projects, "api"),
		"user.name", "Martin", "user.name", "Martin", "user.name", "Martin",
		"user.email", "martin@codechem.com", "user.signingkey", "ABC", "core.sshCommand", "ssh -i ~/.ssh/id_martin")
	repo(t, filepath.Join(projects, "bot"), "user.email", "bot@ci.example", "git-idm.allowAuthor", "bot@ci.example")
	repo(t, filepath.Join(projects, "clean"))
	repo(t, filepath.Join(home, "elsewhere"), "user.email", "martin@codechem.com")

	c := config.New()
	c.Identities["work"] = config.Identity{Name: "Jane", Email: "jane@work.example", SSHKey: "~/.ssh/id_work"}
	c.Rules = []config.Rule{{Path: "~/Projects/", Identity: "work"}}

	scan := ScanRules(c.Rules)
	legacy := LocalLegacy(c, scan.Repos)
	if len(legacy) != 1 {
		t.Fatalf("legacy = %+v", legacy)
	}
	l := legacy[0]
	if l.Kind != LegacyLocal || l.Path != "~/Projects/api" || l.RulePath != "~/Projects/" {
		t.Fatalf("legacy = %+v", l)
	}
	if got := strings.Join(l.Keys, ","); got != "user.name,user.email,core.sshcommand" {
		t.Errorf("keys = %s", got)
	}
	if !strings.Contains(l.Detail, "user.name (3×)") || strings.Contains(l.Detail, "signingkey") || !strings.Contains(l.Detail, "author martin@codechem.com becomes jane@work.example (work)") {
		t.Errorf("detail = %s", l.Detail)
	}
	if l.Config != filepath.Join(projects, "api", ".git", "config") {
		t.Errorf("config = %s", l.Config)
	}
}
