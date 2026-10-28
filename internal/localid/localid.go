package localid

import (
	"bytes"
	"context"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	AllowKey        = "git-idm.allowauthor"
	DefaultDepth    = 4
	DefaultDeadline = 3 * time.Second
)

var Keys = []string{"user.name", "user.email", "user.signingkey", "core.sshcommand"}

var skipDirs = map[string]bool{"node_modules": true, "vendor": true}

type Key struct {
	Name   string
	Values []string
}

type Repo struct {
	Root    string
	Config  string
	Keys    []Key
	Allowed string
}

func (r Repo) Value(name string) string {
	for _, k := range r.Keys {
		if k.Name == name {
			return k.Values[len(k.Values)-1]
		}
	}
	return ""
}

func (r Repo) Count(name string) int {
	for _, k := range r.Keys {
		if k.Name == name {
			return len(k.Values)
		}
	}
	return 0
}

func (r Repo) Overrides() bool {
	return len(r.Keys) > 0
}

type Options struct {
	Depth    int
	Deadline time.Duration
	Exclude  []string
}

type Result struct {
	Repos     []Repo
	Truncated bool
}

func Scan(roots []string, opts Options) Result {
	if opts.Depth <= 0 {
		opts.Depth = DefaultDepth
	}
	if opts.Deadline <= 0 {
		opts.Deadline = DefaultDeadline
	}
	ctx, cancel := context.WithTimeout(context.Background(), opts.Deadline)
	defer cancel()
	s := &scanner{ctx: ctx, opts: opts, seen: map[string]bool{}, excluded: map[string]bool{}}
	for _, ex := range opts.Exclude {
		if filepath.IsAbs(ex) {
			s.excluded[filepath.Clean(ex)] = true
		}
	}
	for _, root := range roots {
		if !filepath.IsAbs(root) {
			continue
		}
		s.walk(filepath.Clean(root))
	}
	sort.Slice(s.result.Repos, func(i, j int) bool { return s.result.Repos[i].Root < s.result.Repos[j].Root })
	return s.result
}

type scanner struct {
	ctx      context.Context
	opts     Options
	seen     map[string]bool
	excluded map[string]bool
	result   Result
}

func (s *scanner) walk(root string) {
	base := strings.Count(root, string(filepath.Separator))
	filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if s.ctx.Err() != nil {
			s.result.Truncated = true
			return fs.SkipAll
		}
		if err != nil || !d.IsDir() {
			return nil
		}
		if path != root && (strings.HasPrefix(d.Name(), ".") || skipDirs[d.Name()] || s.excluded[path]) {
			return fs.SkipDir
		}
		if config := configPath(path); config != "" && !s.seen[config] {
			s.seen[config] = true
			if repo, ok := s.read(path, config); ok {
				s.result.Repos = append(s.result.Repos, repo)
			}
		}
		if strings.Count(path, string(filepath.Separator))-base >= s.opts.Depth {
			return fs.SkipDir
		}
		return nil
	})
}

func (s *scanner) read(root, config string) (Repo, bool) {
	out, err := exec.CommandContext(s.ctx, "git", "config", "--file", config, "--no-includes", "--null", "--list").Output()
	if err != nil {
		if s.ctx.Err() != nil {
			s.result.Truncated = true
		}
		return Repo{}, false
	}
	repo := Repo{Root: root, Config: config}
	values := map[string][]string{}
	for _, rec := range bytes.Split(out, []byte{0}) {
		key, value, _ := strings.Cut(string(rec), "\n")
		key = strings.ToLower(key)
		if key == AllowKey {
			repo.Allowed = value
		}
		values[key] = append(values[key], value)
	}
	for _, name := range Keys {
		if v := values[name]; len(v) > 0 {
			repo.Keys = append(repo.Keys, Key{Name: name, Values: v})
		}
	}
	return repo, true
}

func configPath(dir string) string {
	dotgit := filepath.Join(dir, ".git")
	info, err := os.Lstat(dotgit)
	if err != nil {
		return ""
	}
	gitdir := dotgit
	if !info.IsDir() {
		if gitdir = gitdirFromFile(dotgit); gitdir == "" {
			return ""
		}
	}
	if common, err := os.ReadFile(filepath.Join(gitdir, "commondir")); err == nil {
		target := strings.TrimSpace(string(common))
		if !filepath.IsAbs(target) {
			target = filepath.Join(gitdir, target)
		}
		gitdir = filepath.Clean(target)
	}
	config := filepath.Join(gitdir, "config")
	if info, err := os.Stat(config); err != nil || !info.Mode().IsRegular() {
		return ""
	}
	return config
}

func gitdirFromFile(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	target, ok := strings.CutPrefix(strings.TrimSpace(string(data)), "gitdir:")
	if !ok {
		return ""
	}
	target = strings.TrimSpace(target)
	if !filepath.IsAbs(target) {
		target = filepath.Join(filepath.Dir(path), target)
	}
	return filepath.Clean(target)
}
