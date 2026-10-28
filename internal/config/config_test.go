package config

import (
	"path/filepath"
	"strings"
	"testing"
)

func sample() *Config {
	c := New()
	c.Default = "work"
	c.Identities["work"] = Identity{Name: "Jane Doe", Email: "jane@work.example", GHUser: "jane-work", GHConfigDir: "~/.config/gh"}
	c.Identities["personal"] = Identity{Name: "Jane Doe", Email: "jane@home.example", SSHKey: "~/.ssh/id_personal", Protocol: ProtocolSSH, Color: "green"}
	c.Rules = []Rule{{Path: "~/Personal/", Identity: "personal"}}
	return c
}

func TestRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	want := sample()
	if err := want.SaveFile(path); err != nil {
		t.Fatal(err)
	}
	got, err := LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	a, _ := want.Encode()
	b, _ := got.Encode()
	if string(a) != string(b) {
		t.Fatalf("round trip differs:\n%s\n---\n%s", a, b)
	}
}

func TestLoadMissing(t *testing.T) {
	_, err := LoadFile(filepath.Join(t.TempDir(), "nope.toml"))
	if err == nil || !strings.Contains(err.Error(), "git-idm init") {
		t.Fatalf("want ErrNotFound with hint, got %v", err)
	}
}

func TestValidateRejects(t *testing.T) {
	cases := map[string]func(c *Config){
		"bad identity name":   func(c *Config) { c.Identities["Bad Name"] = c.Identities["work"] },
		"newline in name":     func(c *Config) { id := c.Identities["work"]; id.Name = "x\n[core]"; c.Identities["work"] = id },
		"bad email":           func(c *Config) { id := c.Identities["work"]; id.Email = "nope"; c.Identities["work"] = id },
		"email with display":  func(c *Config) { id := c.Identities["work"]; id.Email = "J <j@x.example>"; c.Identities["work"] = id },
		"bad gh user":         func(c *Config) { id := c.Identities["work"]; id.GHUser = "a b"; c.Identities["work"] = id },
		"bad protocol":        func(c *Config) { id := c.Identities["work"]; id.Protocol = "ftp"; c.Identities["work"] = id },
		"unknown default":     func(c *Config) { c.Default = "ghost" },
		"bad guard":           func(c *Config) { c.Guard = "maybe" },
		"rule unknown id":     func(c *Config) { c.Rules = append(c.Rules, Rule{Path: "~/x/", Identity: "ghost"}) },
		"rule relative path":  func(c *Config) { c.Rules = append(c.Rules, Rule{Path: "x/", Identity: "work"}) },
		"rule no slash":       func(c *Config) { c.Rules = append(c.Rules, Rule{Path: "~/x", Identity: "work"}) },
		"rule quote":          func(c *Config) { c.Rules = append(c.Rules, Rule{Path: "~/x\"/", Identity: "work"}) },
		"rule duplicate path": func(c *Config) { c.Rules = append(c.Rules, Rule{Path: "~/Personal/", Identity: "work"}) },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			c := sample()
			mutate(c)
			if err := c.Validate(); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestExpandContractHome(t *testing.T) {
	t.Setenv("HOME", "/h/u")
	if got := ExpandHome("~/Personal/"); got != "/h/u/Personal/" {
		t.Fatalf("expand: %q", got)
	}
	if got := ContractHome("/h/u/.ssh/id"); got != "~/.ssh/id" {
		t.Fatalf("contract: %q", got)
	}
	if got := ContractHome("/h/user2/x"); got != "/h/user2/x" {
		t.Fatalf("contract prefix: %q", got)
	}
}
