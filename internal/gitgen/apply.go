package gitgen

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/stevetosak/git-idm/internal/config"
)

type Change struct {
	Path   string
	Action string
}

type Applier struct {
	ConfigDir    string
	GlobalConfig string
	Now          func() time.Time
	backupDir    string
	backedUp     map[string]string
}

func NewApplier() (*Applier, error) {
	dir, err := config.Dir()
	if err != nil {
		return nil, err
	}
	global, err := GlobalConfigPath()
	if err != nil {
		return nil, err
	}
	return &Applier{ConfigDir: dir, GlobalConfig: global, Now: time.Now}, nil
}

func GlobalConfigPath() (string, error) {
	if p := os.Getenv("GIT_CONFIG_GLOBAL"); p != "" {
		return p, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".gitconfig"), nil
}

func (a *Applier) GeneratedDir() string {
	return filepath.Join(a.ConfigDir, "generated")
}

func (a *Applier) IdentityFile(name string) string {
	return filepath.Join(a.GeneratedDir(), name+".gitconfig")
}

func (a *Applier) Apply(c *config.Config) ([]Change, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}
	var changes []Change
	if err := os.MkdirAll(a.GeneratedDir(), 0o700); err != nil {
		return nil, err
	}
	for _, name := range c.Names() {
		ch, err := a.writeIfChanged(a.IdentityFile(name), RenderIdentity(c.Identities[name]), false)
		if err != nil {
			return changes, err
		}
		changes = append(changes, ch...)
	}
	stale, err := a.removeStale(c)
	changes = append(changes, stale...)
	if err != nil {
		return changes, err
	}
	current, err := readOptional(a.GlobalConfig)
	if err != nil {
		return changes, err
	}
	block := RenderBlock(c, func(name string) string { return config.ContractHome(a.IdentityFile(name)) })
	updated, err := ReplaceBlock(current, block)
	if err != nil {
		return changes, fmt.Errorf("%s: %w", a.GlobalConfig, err)
	}
	ch, err := a.writeIfChanged(a.GlobalConfig, updated, true)
	return append(changes, ch...), err
}

func (a *Applier) Installed() bool {
	content, err := readOptional(a.GlobalConfig)
	return err == nil && strings.Contains(content, BlockStart)
}

func (a *Applier) Backup(path string) (string, error) {
	if dst, done := a.backedUp[path]; done {
		return dst, nil
	}
	if a.backupDir == "" {
		a.backupDir = filepath.Join(a.ConfigDir, "backups", a.Now().Format("20060102-150405"))
	}
	if err := os.MkdirAll(a.backupDir, 0o700); err != nil {
		return "", err
	}
	name := strings.TrimPrefix(filepath.Base(path), ".")
	if strings.HasSuffix(filepath.Base(path), ".envrc") {
		name = filepath.Base(filepath.Dir(path)) + ".envrc"
	}
	dst := filepath.Join(a.backupDir, name)
	for i := 1; fileExists(dst); i++ {
		dst = filepath.Join(a.backupDir, fmt.Sprintf("%s.%d", name, i))
	}
	if err := copyFile(path, dst); err != nil {
		return "", err
	}
	if a.backedUp == nil {
		a.backedUp = map[string]string{}
	}
	a.backedUp[path] = dst
	return dst, nil
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func (a *Applier) writeIfChanged(path, content string, backup bool) ([]Change, error) {
	current, err := readOptional(path)
	if err != nil {
		return nil, err
	}
	if current == content {
		return nil, nil
	}
	var changes []Change
	if backup && current != "" {
		dst, err := a.Backup(path)
		if err != nil {
			return nil, err
		}
		changes = append(changes, Change{Path: dst, Action: "backup of " + config.ContractHome(path)})
	}
	if err := atomicWrite(path, content); err != nil {
		return changes, err
	}
	action := "updated"
	if current == "" {
		action = "created"
	}
	return append(changes, Change{Path: path, Action: action}), nil
}

func (a *Applier) removeStale(c *config.Config) ([]Change, error) {
	entries, err := os.ReadDir(a.GeneratedDir())
	if err != nil {
		return nil, err
	}
	var changes []Change
	for _, e := range entries {
		name, ok := strings.CutSuffix(e.Name(), ".gitconfig")
		if !ok {
			continue
		}
		if _, keep := c.Identities[name]; keep {
			continue
		}
		path := filepath.Join(a.GeneratedDir(), e.Name())
		if err := os.Remove(path); err != nil {
			return changes, err
		}
		changes = append(changes, Change{Path: path, Action: "removed"})
	}
	return changes, nil
}

func readOptional(path string) (string, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	return string(data), err
}

func atomicWrite(path, content string) error {
	mode := os.FileMode(0o644)
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm()
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.WriteString(content); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
