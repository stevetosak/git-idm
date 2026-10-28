package config

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/BurntSushi/toml"
)

type Protocol string

const (
	ProtocolHTTPS Protocol = "https"
	ProtocolSSH   Protocol = "ssh"
)

type Identity struct {
	Name        string   `toml:"name"`
	Email       string   `toml:"email"`
	SSHKey      string   `toml:"ssh_key,omitempty"`
	SigningKey  string   `toml:"signing_key,omitempty"`
	GHUser      string   `toml:"gh_user,omitempty"`
	GHConfigDir string   `toml:"gh_config_dir,omitempty"`
	Protocol    Protocol `toml:"protocol,omitempty"`
	Color       string   `toml:"color,omitempty"`
}

type Rule struct {
	Path      string `toml:"path"`
	Identity  string `toml:"identity"`
	Temporary bool   `toml:"temporary,omitempty"`
	Was       string `toml:"was,omitempty"`
}

type GuardMode string

const (
	GuardOff   GuardMode = "off"
	GuardWarn  GuardMode = "warn"
	GuardBlock GuardMode = "block"
)

type Config struct {
	Default    string              `toml:"default"`
	Guard      GuardMode           `toml:"guard,omitempty"`
	Identities map[string]Identity `toml:"identities"`
	Rules      []Rule              `toml:"rules"`
}

var ErrNotFound = errors.New("config not found")

func New() *Config {
	return &Config{Identities: map[string]Identity{}}
}

func Dir() (string, error) {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "git-idm"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "git-idm"), nil
}

func Path() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.toml"), nil
}

func Load() (*Config, error) {
	path, err := Path()
	if err != nil {
		return nil, err
	}
	return LoadFile(path)
}

func LoadFile(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("%w: %s (run `git-idm init`)", ErrNotFound, path)
	}
	if err != nil {
		return nil, err
	}
	c := New()
	if _, err := toml.Decode(string(data), c); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if c.Identities == nil {
		c.Identities = map[string]Identity{}
	}
	if err := c.Validate(); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return c, nil
}

func (c *Config) Save() error {
	path, err := Path()
	if err != nil {
		return err
	}
	return c.SaveFile(path)
}

func (c *Config) SaveFile(path string) error {
	if err := c.Validate(); err != nil {
		return err
	}
	data, err := c.Encode()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func (c *Config) Encode() ([]byte, error) {
	var buf bytes.Buffer
	if err := toml.NewEncoder(&buf).Encode(c); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func (c *Config) Names() []string {
	names := make([]string, 0, len(c.Identities))
	for name := range c.Identities {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func (c *Config) GuardEnabled() bool {
	return c.Guard == GuardWarn || c.Guard == GuardBlock
}

func (c *Config) RulesFor(identity string) []Rule {
	var rules []Rule
	for _, r := range c.Rules {
		if r.Identity == identity {
			rules = append(rules, r)
		}
	}
	return rules
}
