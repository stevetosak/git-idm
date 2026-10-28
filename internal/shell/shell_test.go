package shell

import (
	"testing"

	"github.com/stevetosak/git-idm/internal/config"
	"github.com/stevetosak/git-idm/internal/resolve"
)

func TestSegment(t *testing.T) {
	c := config.New()
	c.Identities["work"] = config.Identity{Name: "W", Email: "w@x.example", Color: "green"}
	c.Identities["client"] = config.Identity{Name: "C", Email: "c@x.example", Color: "magenta"}
	normal := config.Rule{Path: "~/Work/", Identity: "work"}
	temp := config.Rule{Path: "~/Work/", Identity: "client", Temporary: true, Was: "work"}
	cases := []struct {
		res  resolve.Result
		want string
	}{
		{resolve.Result{Identity: "work", Source: resolve.SourceRule, Rule: &normal}, "%F{green}work%f"},
		{resolve.Result{Identity: "client", Source: resolve.SourceRule, Rule: &temp}, "%B%F{yellow}client (temp)%f%b"},
		{resolve.Result{Identity: "work", Source: resolve.SourceOverride, Rule: &temp}, "%B%F{red}work (override)%f%b"},
		{resolve.Result{Identity: "client", Source: resolve.SourceRule, Rule: &temp, LocalEmail: "o@x.example"}, "%B%F{yellow}client (temp) (repo user)%f%b"},
		{resolve.Result{}, ""},
	}
	for _, tc := range cases {
		if got := Segment(Zsh, c, tc.res); got != tc.want {
			t.Errorf("Segment(%+v) = %q, want %q", tc.res, got, tc.want)
		}
	}
}

func TestFishOutput(t *testing.T) {
	t.Setenv(resolve.OverrideEnv, "")
	c := config.New()
	c.Identities["it's"] = config.Identity{Name: "W", Email: "w@x.example", GHConfigDir: `/h/gh \ it's`}
	res := resolve.Result{Identity: "it's", Source: resolve.SourceRule}
	want := "set -gx GH_CONFIG_DIR '/h/gh \\\\ it\\'s'\nset -gx _GIT_IDM_SEGMENT '\x1b[36mit\\'s\x1b[0m'\n"
	if got := Sync(Fish, c, res); got != want {
		t.Errorf("Sync:\n got  %q\n want %q", got, want)
	}
	if got := Sync(Fish, c, resolve.Result{}); got != "set -e GH_CONFIG_DIR\nset -gx _GIT_IDM_SEGMENT ''\n" {
		t.Errorf("Sync without identity: %q", got)
	}
}
