package setup

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/stevetosak/git-idm/internal/config"
)

func foundConfig() *config.Config {
	c := config.New()
	c.Default = "work"
	c.Identities["work"] = config.Identity{Name: "Jane", Email: "jane@work.example", GHConfigDir: "~/.config/gh", GHUser: "jane-work"}
	c.Identities["personal"] = config.Identity{Name: "Jane", Email: "jane@home.example", SSHKey: "~/.ssh/id_p"}
	c.Identities["projects"] = config.Identity{Name: "Old", Email: "old@x.example"}
	c.Rules = []config.Rule{{Path: "~/Personal/", Identity: "personal"}, {Path: "~/Projects/", Identity: "projects"}, {Path: "~/Other/", Identity: "personal"}}
	return c
}

func TestReviewRenamesSkipsAndPicksDefault(t *testing.T) {
	f := &fakePrompter{t: t, answers: map[string][]string{
		"Identity personal":              {"keep"},
		"Identity projects":              {"skip"},
		"Identity work":                  {"rename"},
		"New name for work":              {"Bad Name", "personal", "acme"},
		"Default identity":               {"<default>"},
		"Save this config and apply it?": {"<default>"},
	}}
	c, err := (&Reviewer{Prompt: f}).Run(foundConfig())
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(c.Names(), ","); got != "acme,personal" {
		t.Fatalf("identities = %s", got)
	}
	if c.Default != "acme" || c.Identities["acme"].GHUser != "jane-work" {
		t.Fatalf("the default must follow the rename: %q %+v", c.Default, c.Identities["acme"])
	}
	want := []config.Rule{{Path: "~/Personal/", Identity: "personal"}, {Path: "~/Other/", Identity: "personal"}}
	if !reflect.DeepEqual(c.Rules, want) {
		t.Fatalf("rules = %+v", c.Rules)
	}
	if !reflect.DeepEqual(f.errors, []string{"New name for work: use 1-32 characters: a-z, 0-9, - or _", "New name for work: another imported identity is already called personal"}) {
		t.Fatalf("errors = %v", f.errors)
	}
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	summary := f.notes["Summary"]
	for _, want := range []string{"acme\n  author   Jane <jane@work.example>\n  used in  ★ default (folders without a rule)\n  gh       jane-work (~/.config/gh)", "personal\n  author   Jane <jane@home.example>\n  used in  ~/Personal/, ~/Other/\n  ssh      ~/.ssh/id_p"} {
		if !strings.Contains(summary, want) {
			t.Errorf("summary misses %q:\n%s", want, summary)
		}
	}
	if !strings.Contains(f.notes["Found your git setup"], "found 3 identities") {
		t.Errorf("found note:\n%s", f.notes["Found your git setup"])
	}
}

func TestReviewNoDefaultAndAbort(t *testing.T) {
	f := &fakePrompter{t: t, answers: map[string][]string{
		"Identity personal":              {"keep"},
		"Identity projects":              {"keep"},
		"Identity work":                  {"keep"},
		"Default identity":               {"none"},
		"Save this config and apply it?": {"<default>"},
	}}
	c, err := (&Reviewer{Prompt: f}).Run(foundConfig())
	if err != nil || c.Default != "" || !strings.Contains(f.notes["Summary"], "No default") {
		t.Fatalf("%+v %v\n%s", c, err, f.notes["Summary"])
	}

	f = &fakePrompter{t: t, answers: map[string][]string{
		"Identity personal": {"skip"}, "Identity projects": {"skip"}, "Identity work": {"skip"},
	}}
	if _, err := (&Reviewer{Prompt: f}).Run(foundConfig()); !errors.Is(err, ErrAborted) {
		t.Fatalf("skipping all must import nothing: %v", err)
	}

	f = &fakePrompter{t: t, answers: map[string][]string{
		"Identity personal": {"keep"}, "Identity projects": {"keep"}, "Identity work": {"keep"},
		"Default identity": {"<default>"}, "Save this config and apply it?": {"no"},
	}}
	if _, err := (&Reviewer{Prompt: f}).Run(foundConfig()); !errors.Is(err, ErrAborted) {
		t.Fatalf("err = %v", err)
	}
}
