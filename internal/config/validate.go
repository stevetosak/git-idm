package config

import (
	"fmt"
	"net/mail"
	"regexp"
	"strings"
	"unicode"
)

var (
	identityNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,31}$`)
	ghUserPattern       = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9-]{0,38})$`)
	colors              = map[string]bool{"": true, "red": true, "green": true, "yellow": true, "blue": true, "magenta": true, "cyan": true, "white": true}
)

func ValidateIdentityName(name string) error {
	if !identityNamePattern.MatchString(name) {
		return fmt.Errorf("identity name %q: use 1-32 chars of a-z, 0-9, '-', '_', starting with a letter or digit", name)
	}
	return nil
}

func (c *Config) Validate() error {
	for _, name := range c.Names() {
		if err := ValidateIdentityName(name); err != nil {
			return err
		}
		if err := c.Identities[name].Validate(); err != nil {
			return fmt.Errorf("identity %q: %w", name, err)
		}
	}
	if c.Default != "" {
		if _, ok := c.Identities[c.Default]; !ok {
			return fmt.Errorf("default identity %q does not exist", c.Default)
		}
	}
	switch c.Guard {
	case "", GuardOff, GuardWarn, GuardBlock:
	default:
		return fmt.Errorf("guard %q: use %q, %q or %q", c.Guard, GuardOff, GuardWarn, GuardBlock)
	}
	seen := map[string]bool{}
	for _, r := range c.Rules {
		if err := r.Validate(); err != nil {
			return err
		}
		if _, ok := c.Identities[r.Identity]; !ok {
			return fmt.Errorf("rule %q: identity %q does not exist", r.Path, r.Identity)
		}
		if seen[r.Path] {
			return fmt.Errorf("rule %q: defined more than once", r.Path)
		}
		seen[r.Path] = true
	}
	return nil
}

func (id Identity) Validate() error {
	fields := map[string]string{
		"name": id.Name, "email": id.Email, "ssh_key": id.SSHKey,
		"signing_key": id.SigningKey, "gh_user": id.GHUser, "gh_config_dir": id.GHConfigDir,
	}
	for field, value := range fields {
		if err := safeValue(value); err != nil {
			return fmt.Errorf("%s: %w", field, err)
		}
	}
	if strings.TrimSpace(id.Name) == "" {
		return fmt.Errorf("name is required")
	}
	if addr, err := mail.ParseAddress(id.Email); err != nil || addr.Address != id.Email {
		return fmt.Errorf("email %q is not a valid address", id.Email)
	}
	if id.GHUser != "" && !ghUserPattern.MatchString(id.GHUser) {
		return fmt.Errorf("gh_user %q is not a valid GitHub login", id.GHUser)
	}
	switch id.Protocol {
	case "", ProtocolHTTPS, ProtocolSSH:
	default:
		return fmt.Errorf("protocol %q: use %q or %q", id.Protocol, ProtocolHTTPS, ProtocolSSH)
	}
	if !colors[id.Color] {
		return fmt.Errorf("color %q is not supported", id.Color)
	}
	return nil
}

func (r Rule) Validate() error {
	if err := safeValue(r.Path); err != nil {
		return fmt.Errorf("rule path: %w", err)
	}
	if !strings.HasPrefix(r.Path, "/") && !strings.HasPrefix(r.Path, "~/") {
		return fmt.Errorf("rule path %q: must be absolute or start with ~/", r.Path)
	}
	if !strings.HasSuffix(r.Path, "/") {
		return fmt.Errorf("rule path %q: must end with /", r.Path)
	}
	if strings.ContainsAny(r.Path, "\"\\") {
		return fmt.Errorf("rule path %q: must not contain quotes or backslashes", r.Path)
	}
	return nil
}

func safeValue(v string) error {
	for _, r := range v {
		if unicode.IsControl(r) {
			return fmt.Errorf("contains a control character")
		}
	}
	return nil
}
