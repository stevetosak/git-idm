package ghauth

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

var loginFiles = []string{"hosts.yml", "config.yml"}

func CopyLogin(from, to string) error {
	if !filepath.IsAbs(from) || !filepath.IsAbs(to) {
		return errors.New("gh login folders must be absolute paths")
	}
	if filepath.Clean(from) == filepath.Clean(to) {
		return errors.New("source and target gh folders are the same")
	}
	if info, err := os.Lstat(filepath.Join(from, "hosts.yml")); err != nil || !info.Mode().IsRegular() {
		return fmt.Errorf("%s has no gh login", from)
	}
	if _, err := os.Lstat(filepath.Join(to, "hosts.yml")); err == nil {
		return fmt.Errorf("%s already has a gh login", to)
	}
	if err := os.MkdirAll(to, 0o700); err != nil {
		return err
	}
	if err := os.Chmod(to, 0o700); err != nil {
		return err
	}
	for _, name := range loginFiles {
		src := filepath.Join(from, name)
		info, err := os.Lstat(src)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("%s is not a regular file", src)
		}
		data, err := os.ReadFile(src)
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(to, name), data, 0o600); err != nil {
			return err
		}
	}
	return nil
}
