package setup

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/stevetosak/git-idm/internal/config"
)

func editConfig() *config.Config {
	c := existing()
	c.Identities["work"] = config.Identity{Name: "Jane", Email: "jane@work.example", SSHKey: "~/.ssh/id_w", Color: "green"}
	c.Identities["personal"] = config.Identity{Name: "Jane", Email: "jane@home.example"}
	return c
}

func TestEditorChangesFields(t *testing.T) {
	f := &fakePrompter{t: t, answers: map[string][]string{
		"Edit work":                {"email", "name", "color", "default", "email", "done"},
		"Email":                    {"jane@home.example", "Jane@Work.example", "<default>"},
		"Author name":              {"", "Jane Doe"},
		"Prompt color":             {"magenta"},
		"Use work as the default?": {"no"},
	}}
	edit, err := (&Editor{Prompt: f, Config: editConfig()}).Run("work")
	if err != nil {
		t.Fatal(err)
	}
	want := config.Identity{Name: "Jane Doe", Email: "Jane@Work.example", SSHKey: "~/.ssh/id_w", Color: "magenta"}
	if edit.Identity != want || edit.Default {
		t.Fatalf("edit = %+v", edit)
	}
	if got := strings.Join(edit.Changed, ","); got != "author name,email,color,default" {
		t.Fatalf("changed = %s", got)
	}
	wantErrors := []string{`Email: identity "personal" already uses this email`, "Author name: enter the author name"}
	if !reflect.DeepEqual(f.errors, wantErrors) {
		t.Fatalf("errors = %v", f.errors)
	}
	menu := f.choices["Edit work"]
	if menu[0].Label != "Author name  Jane Doe" || menu[2].Label != "Color        magenta" || menu[3].Label != "Default      no" {
		t.Fatalf("menu = %+v", menu)
	}
}

func TestEditorCancelAndNoChanges(t *testing.T) {
	f := &fakePrompter{t: t, answers: map[string][]string{
		"Edit work":    {"color", "cancel"},
		"Prompt color": {"red"},
	}}
	if _, err := (&Editor{Prompt: f, Config: editConfig()}).Run("work"); !errors.Is(err, ErrAborted) {
		t.Fatalf("err = %v", err)
	}
	f = &fakePrompter{t: t, answers: map[string][]string{
		"Edit personal": {"color", "done"},
		"Prompt color":  {"<default>"},
	}}
	edit, err := (&Editor{Prompt: f, Config: editConfig()}).Run("personal")
	if err != nil || len(edit.Changed) != 0 {
		t.Fatalf("an unset color stays cyan, no change: %+v %v", edit, err)
	}
	if _, err := (&Editor{Prompt: f, Config: editConfig()}).Run("nope"); err == nil {
		t.Fatal("want error for an unknown identity")
	}
}
