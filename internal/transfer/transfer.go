package transfer

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/stevetosak/git-idm/internal/config"
)

const (
	Format  = "git-idm/identities"
	Version = 1
	MaxSize = 64 << 10

	SigningSSH = "ssh"
	SigningGPG = "gpg"
)

var gpgKeyPattern = regexp.MustCompile(`^[0-9A-Fa-f]{8,40}!?$`)

type File struct {
	Format     string  `toml:"format"`
	Version    int     `toml:"version"`
	Identities []Entry `toml:"identity"`
}

type Entry struct {
	ID         string          `toml:"id"`
	Name       string          `toml:"name"`
	Email      string          `toml:"email"`
	Color      string          `toml:"color,omitempty"`
	Protocol   config.Protocol `toml:"protocol,omitempty"`
	GitHubUser string          `toml:"github_user,omitempty"`
	Signing    string          `toml:"signing,omitempty"`
	GPGKey     string          `toml:"gpg_key,omitempty"`
}

func FromConfig(c *config.Config, names []string) (File, error) {
	f := File{Format: Format, Version: Version}
	for _, name := range names {
		id, ok := c.Identities[name]
		if !ok {
			return File{}, fmt.Errorf("identity %q does not exist", name)
		}
		e := Entry{ID: name, Name: id.Name, Email: id.Email, Color: id.Color, Protocol: id.Protocol, GitHubUser: id.GHUser}
		e.Signing, e.GPGKey = signingOf(id.SigningKey)
		f.Identities = append(f.Identities, e)
	}
	return f, f.Validate()
}

func signingOf(key string) (kind, gpgKey string) {
	switch {
	case key == "":
		return "", ""
	case strings.HasSuffix(key, ".pub") || strings.HasPrefix(key, "ssh-") || strings.HasPrefix(key, "key::"):
		return SigningSSH, ""
	case gpgKeyPattern.MatchString(key):
		return SigningGPG, key
	}
	return SigningGPG, ""
}

func (f File) Encode() ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteString("# git-idm identities. No SSH keys, tokens or folders.\n# Import with: git-idm import <file>\n\n")
	if err := toml.NewEncoder(&buf).Encode(f); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func Read(r io.Reader) (File, error) {
	data, err := io.ReadAll(io.LimitReader(r, MaxSize+1))
	if err != nil {
		return File{}, err
	}
	if len(data) > MaxSize {
		return File{}, fmt.Errorf("the file is larger than %d KB, it is not a git-idm export", MaxSize>>10)
	}
	return Decode(data)
}

func ReadFile(path string) (File, error) {
	fh, err := os.Open(path)
	if err != nil {
		return File{}, err
	}
	defer fh.Close()
	info, err := fh.Stat()
	if err != nil {
		return File{}, err
	}
	if !info.Mode().IsRegular() {
		return File{}, fmt.Errorf("%s is not a regular file", path)
	}
	return Read(fh)
}

func Decode(data []byte) (File, error) {
	var f File
	meta, err := toml.Decode(string(data), &f)
	if err != nil {
		return File{}, fmt.Errorf("not a valid git-idm export: %w", err)
	}
	if f.Format != Format {
		return File{}, errors.New("not a git-idm export (format line is missing or wrong)")
	}
	if f.Version != Version {
		return File{}, fmt.Errorf("export version %d is not supported, this git-idm reads version %d (run: git-idm update)", f.Version, Version)
	}
	if undecoded := meta.Undecoded(); len(undecoded) > 0 {
		return File{}, fmt.Errorf("unknown field %q in the export", undecoded[0].String())
	}
	return f, f.Validate()
}

func (f File) Validate() error {
	if len(f.Identities) == 0 {
		return errors.New("the export has no identities")
	}
	seen := map[string]bool{}
	for _, e := range f.Identities {
		if err := e.Validate(); err != nil {
			return err
		}
		if seen[e.ID] {
			return fmt.Errorf("identity %q is in the export more than once", e.ID)
		}
		seen[e.ID] = true
	}
	return nil
}

func (e Entry) Validate() error {
	if err := config.ValidateIdentityName(e.ID); err != nil {
		return err
	}
	id := config.Identity{Name: e.Name, Email: e.Email, Color: e.Color, Protocol: e.Protocol, GHUser: e.GitHubUser}
	if err := id.Validate(); err != nil {
		return fmt.Errorf("identity %q: %w", e.ID, err)
	}
	switch e.Signing {
	case "", SigningSSH:
		if e.GPGKey != "" {
			return fmt.Errorf("identity %q: gpg_key needs signing = %q", e.ID, SigningGPG)
		}
	case SigningGPG:
		if e.GPGKey != "" && !gpgKeyPattern.MatchString(e.GPGKey) {
			return fmt.Errorf("identity %q: gpg_key %q is not a gpg key ID", e.ID, e.GPGKey)
		}
	default:
		return fmt.Errorf("identity %q: signing %q: use %q or %q", e.ID, e.Signing, SigningSSH, SigningGPG)
	}
	return nil
}
