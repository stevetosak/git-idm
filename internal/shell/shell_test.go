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
		if got := Segment(c, tc.res); got != tc.want {
			t.Errorf("Segment(%+v) = %q, want %q", tc.res, got, tc.want)
		}
	}
}
