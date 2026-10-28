package transfer

import (
	"strings"
	"testing"

	"github.com/stevetosak/git-idm/internal/config"
)

func sampleConfig() *config.Config {
	c := config.New()
	c.Identities["work"] = config.Identity{Name: "Ana Work", Email: "ana@corp.com", SSHKey: "~/.ssh/id_work",
		SigningKey: "~/.ssh/id_work.pub", GHUser: "ana-corp", GHConfigDir: "~/.config/gh-work", Protocol: config.ProtocolSSH, Color: "blue"}
	c.Identities["personal"] = config.Identity{Name: "Ana", Email: "ana@mail.com", SigningKey: "3AA5C34371567BD2"}
	c.Identities["old"] = config.Identity{Name: "Ana", Email: "ana@old.com", SigningKey: "ana@old.com"}
	c.Rules = []config.Rule{{Path: "~/Projects/work/", Identity: "work"}}
	return c
}

func TestExportRoundTripWithoutSecrets(t *testing.T) {
	f, err := FromConfig(sampleConfig(), []string{"old", "personal", "work"})
	if err != nil {
		t.Fatal(err)
	}
	data, err := f.Encode()
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, secret := range []string{"id_work", "gh-work", "Projects", "gpg_key = \"ana@old.com\""} {
		if strings.Contains(text, secret) {
			t.Errorf("the export contains %q:\n%s", secret, text)
		}
	}
	got, err := Decode(data)
	if err != nil {
		t.Fatalf("%v\n%s", err, text)
	}
	want := map[string]Entry{
		"old":      {ID: "old", Name: "Ana", Email: "ana@old.com", Signing: SigningGPG},
		"personal": {ID: "personal", Name: "Ana", Email: "ana@mail.com", Signing: SigningGPG, GPGKey: "3AA5C34371567BD2"},
		"work":     {ID: "work", Name: "Ana Work", Email: "ana@corp.com", Color: "blue", Protocol: config.ProtocolSSH, GitHubUser: "ana-corp", Signing: SigningSSH},
	}
	for _, e := range got.Identities {
		if e != want[e.ID] {
			t.Errorf("%s: got %+v, want %+v", e.ID, e, want[e.ID])
		}
	}
}

func TestFromConfigUnknownIdentity(t *testing.T) {
	if _, err := FromConfig(sampleConfig(), []string{"nope"}); err == nil {
		t.Fatal("want an error")
	}
}

func TestDecodeRejectsBadFiles(t *testing.T) {
	head := "format = \"git-idm/identities\"\nversion = 1\n"
	entry := "[[identity]]\nid = \"work\"\nname = \"Ana\"\nemail = \"ana@corp.com\"\n"
	cases := map[string]string{
		"not toml":         "{{{",
		"no format":        "version = 1\n" + entry,
		"wrong format":     "format = \"other\"\nversion = 1\n" + entry,
		"newer version":    "format = \"git-idm/identities\"\nversion = 2\n" + entry,
		"empty":            head,
		"unknown field":    head + entry + "ssh_key = \"~/.ssh/id\"\n",
		"bad id":           head + strings.Replace(entry, "\"work\"", "\"../x\"", 1),
		"bad email":        head + strings.Replace(entry, "ana@corp.com", "not an email", 1),
		"control char":     head + strings.Replace(entry, "\"Ana\"", "\"Ana\\u001b[31m\"", 1),
		"bad color":        head + entry + "color = \"pink\"\n",
		"bad protocol":     head + entry + "protocol = \"ftp\"\n",
		"bad github user":  head + entry + "github_user = \"a b\"\n",
		"bad signing":      head + entry + "signing = \"x509\"\n",
		"bad gpg key":      head + entry + "signing = \"gpg\"\ngpg_key = \"$(rm -rf)\"\n",
		"gpg key with ssh": head + entry + "signing = \"ssh\"\ngpg_key = \"3AA5C34371567BD2\"\n",
		"duplicate id":     head + entry + entry,
	}
	for name, input := range cases {
		if _, err := Decode([]byte(input)); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
}

func TestReadRejectsLargeFiles(t *testing.T) {
	if _, err := Read(strings.NewReader(strings.Repeat("#", MaxSize+1))); err == nil || !strings.Contains(err.Error(), "larger") {
		t.Fatalf("got %v", err)
	}
}
