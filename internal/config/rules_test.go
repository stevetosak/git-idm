package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func ruleConfig() *Config {
	c := New()
	for _, name := range []string{"work", "client", "personal"} {
		c.Identities[name] = Identity{Name: "J", Email: "j@" + name + ".example"}
	}
	c.Rules = []Rule{{Path: "~/Work/", Identity: "work"}}
	return c
}

func TestBindTemporary(t *testing.T) {
	c := ruleConfig()
	r, err := c.Bind("~/Work/", "client", true)
	if err != nil || r != (Rule{Path: "~/Work/", Identity: "client", Temporary: true, Was: "work"}) {
		t.Fatalf("%+v %v", r, err)
	}
	if r, _ := c.Bind("~/Work/", "personal", true); r.Was != "work" || r.Identity != "personal" {
		t.Fatalf("a second temporary bind must keep the old identity: %+v", r)
	}
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	old, err := c.Unbind("~/Work/")
	if err != nil || old.Identity != "personal" || !reflect.DeepEqual(c.Rules, []Rule{{Path: "~/Work/", Identity: "work"}}) {
		t.Fatalf("unbind: %+v %v rules=%+v", old, err, c.Rules)
	}
	if _, err := c.Bind("~/Work/", "work", true); err == nil || !strings.Contains(err.Error(), "already uses work") {
		t.Fatalf("err = %v", err)
	}
}

func TestBindTemporaryEnds(t *testing.T) {
	c := ruleConfig()
	c.Bind("~/Work/", "client", true)
	if r, _ := c.Bind("~/Work/", "work", true); r != (Rule{Path: "~/Work/", Identity: "work"}) {
		t.Fatalf("binding back to the old identity ends it: %+v", r)
	}
	c.Bind("~/Work/", "client", true)
	if r, _ := c.Bind("~/Work/", "personal", false); r != (Rule{Path: "~/Work/", Identity: "personal"}) {
		t.Fatalf("a normal bind makes it permanent: %+v", r)
	}
	c.Bind("~/New/", "client", true)
	if old, err := c.Unbind("~/New/"); err != nil || !old.Temporary || len(c.Rules) != 1 {
		t.Fatalf("a temporary rule without an old identity is removed: %+v %v %+v", old, err, c.Rules)
	}
	if _, err := c.Bind("~/X/", "nope", true); err == nil {
		t.Fatal("want error for an unknown identity")
	}
	if _, err := c.Unbind("~/Nothing/"); err == nil {
		t.Fatal("want error without a rule")
	}
}

func TestRemoveRulesForKeepsTemporaryState(t *testing.T) {
	c := ruleConfig()
	c.Rules = []Rule{
		{Path: "~/A/", Identity: "client", Temporary: true, Was: "work"},
		{Path: "~/B/", Identity: "personal", Temporary: true, Was: "client"},
		{Path: "~/C/", Identity: "client"},
		{Path: "~/D/", Identity: "work"},
	}
	c.RemoveRulesFor("client")
	delete(c.Identities, "client")
	want := []Rule{
		{Path: "~/A/", Identity: "work"},
		{Path: "~/B/", Identity: "personal", Temporary: true},
		{Path: "~/D/", Identity: "work"},
	}
	if !reflect.DeepEqual(c.Rules, want) {
		t.Fatalf("rules = %+v", c.Rules)
	}
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestValidateTemporaryRule(t *testing.T) {
	for _, r := range []Rule{
		{Path: "~/A/", Identity: "client", Was: "work"},
		{Path: "~/A/", Identity: "client", Temporary: true, Was: "nope"},
		{Path: "~/A/", Identity: "client", Temporary: true, Was: "client"},
	} {
		c := ruleConfig()
		c.Rules = []Rule{r}
		if err := c.Validate(); err == nil {
			t.Errorf("want error for %+v", r)
		}
	}
}

func TestTemporaryRuleRoundTrip(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	c := ruleConfig()
	c.Bind("~/Work/", "client", true)
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "git-idm", "config.toml"))
	if !strings.Contains(string(data), "temporary = true") || !strings.Contains(string(data), `was = "work"`) {
		t.Fatalf("config.toml:\n%s", data)
	}
	loaded, err := Load()
	if err != nil || !reflect.DeepEqual(loaded.Rules, c.Rules) {
		t.Fatalf("%+v %v", loaded.Rules, err)
	}
	loaded.Unbind("~/Work/")
	loaded.Save()
	if data, _ := os.ReadFile(filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "git-idm", "config.toml")); strings.Contains(string(data), "temporary") || strings.Contains(string(data), "was") {
		t.Fatalf("a normal rule must not write the fields:\n%s", data)
	}
}
