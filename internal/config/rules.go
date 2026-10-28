package config

import (
	"fmt"
	"slices"
)

func (c *Config) ruleIndex(path string) int {
	return slices.IndexFunc(c.Rules, func(r Rule) bool { return r.Path == path })
}

func (c *Config) Bind(path, name string, temporary bool) (Rule, error) {
	if _, ok := c.Identities[name]; !ok {
		return Rule{}, fmt.Errorf("identity %q does not exist", name)
	}
	i := c.ruleIndex(path)
	if i < 0 {
		c.Rules = append(c.Rules, Rule{Path: path, Identity: name, Temporary: temporary})
		return c.Rules[len(c.Rules)-1], nil
	}
	r := &c.Rules[i]
	switch {
	case !temporary, r.Temporary && name == r.Was:
		*r = Rule{Path: path, Identity: name}
	case r.Temporary:
		r.Identity = name
	case r.Identity == name:
		return Rule{}, fmt.Errorf("%s already uses %s", path, name)
	default:
		*r = Rule{Path: path, Identity: name, Temporary: true, Was: r.Identity}
	}
	return *r, nil
}

func (c *Config) Unbind(path string) (Rule, error) {
	i := c.ruleIndex(path)
	if i < 0 {
		return Rule{}, fmt.Errorf("no rule for %s", path)
	}
	old := c.Rules[i]
	if old.Temporary && old.Was != "" {
		c.Rules[i] = Rule{Path: path, Identity: old.Was}
		return old, nil
	}
	c.Rules = slices.Delete(c.Rules, i, i+1)
	return old, nil
}

func (c *Config) RemoveRulesFor(name string) {
	var kept []Rule
	for _, r := range c.Rules {
		switch {
		case r.Identity == name && r.Temporary && r.Was != "":
			kept = append(kept, Rule{Path: r.Path, Identity: r.Was})
		case r.Identity == name:
		case r.Was == name:
			r.Was = ""
			kept = append(kept, r)
		default:
			kept = append(kept, r)
		}
	}
	c.Rules = kept
}
