package main

import (
	"io"
	"strings"
	"testing"

	"github.com/stevetosak/git-idm/internal/config"
)

func TestArgumentErrorsExplainUsage(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	c := config.New()
	c.Identities["work"] = config.Identity{Name: "W", Email: "w@x.example"}
	c.Identities["client"] = config.Identity{Name: "C", Email: "c@x.example"}
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		args []string
		want []string
		not  []string
	}{
		{[]string{"edit"}, []string{"edit needs <identity>", "usage:       git-idm edit <identity> [flags]", "example:     git-idm edit client", "identities:  client, work", "help:        git-idm edit --help"}, nil},
		{[]string{"bind"}, []string{"bind needs <identity>", "usage:       git-idm bind <identity> [dir] [flags]"}, nil},
		{[]string{"rm", "a", "b"}, []string{"rm got too many arguments: b"}, nil},
		{[]string{"ls", "x"}, []string{"ls takes no arguments, got: x", "help:        git-idm ls --help"}, []string{"identities"}},
		{[]string{"hook"}, []string{"hook needs <zsh|bash|fish>"}, []string{"identities"}},
		{[]string{"ls", "--nope"}, []string{"unknown flag: --nope", "usage:       git-idm ls [flags]"}, nil},
		{[]string{"use", "--shell"}, []string{"use needs <identity> or --clear", "identities:  client, work"}, nil},
	}
	for _, tc := range cases {
		root := newRootCmd()
		root.SetOut(io.Discard)
		root.SetErr(io.Discard)
		root.SetArgs(tc.args)
		err := root.Execute()
		if err == nil {
			t.Errorf("%v: want an error", tc.args)
			continue
		}
		for _, want := range tc.want {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("%v: missing %q in:\n%s", tc.args, want, err)
			}
		}
		for _, not := range tc.not {
			if strings.Contains(err.Error(), not) {
				t.Errorf("%v: must not contain %q:\n%s", tc.args, not, err)
			}
		}
	}
}

func TestArgNames(t *testing.T) {
	cases := map[string][2]int{
		"edit <identity>":          {1, 0},
		"bind <identity> [dir]":    {1, 1},
		"use <identity> | --clear": {1, 0},
		"hook zsh":                 {1, 0},
		"guard [block|warn|off]":   {0, 1},
		"ls":                       {0, 0},
	}
	for use, want := range cases {
		required, optional := argNames(use)
		if len(required) != want[0] || len(optional) != want[1] {
			t.Errorf("%s: %v %v", use, required, optional)
		}
	}
}
