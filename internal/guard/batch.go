package guard

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	pendingFile  = "pending"
	acceptedFile = "accepted"
)

func batchDir(dir string) (string, error) {
	common, err := git(dir, "rev-parse", "--git-common-dir")
	if err != nil {
		return "", err
	}
	if common == "" {
		return "", errors.New("not inside a git repository")
	}
	if !filepath.IsAbs(common) {
		common = filepath.Join(dir, common)
	}
	return filepath.Join(common, "git-idm"), nil
}

func readSHAs(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var shas []string
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if shaPattern.MatchString(line) {
			shas = append(shas, line)
		}
	}
	return shas, nil
}

func writeSHAs(path string, shas []string) error {
	if len(shas) == 0 {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(strings.Join(shas, "\n")+"\n"), 0o600)
}

func batchFile(dir, name string) (string, error) {
	d, err := batchDir(dir)
	if err != nil {
		return "", err
	}
	return filepath.Join(d, name), nil
}

func SavePending(dir string, shas []string) error {
	path, err := batchFile(dir, pendingFile)
	if err != nil {
		return err
	}
	return writeSHAs(path, shas)
}

func Pending(dir string) ([]string, error) {
	path, err := batchFile(dir, pendingFile)
	if err != nil {
		return nil, err
	}
	return readSHAs(path)
}

func AcceptedCommits(dir string) (map[string]bool, error) {
	path, err := batchFile(dir, acceptedFile)
	if err != nil {
		return nil, err
	}
	shas, err := readSHAs(path)
	if err != nil {
		return nil, err
	}
	accepted := map[string]bool{}
	for _, sha := range shas {
		accepted[sha] = true
	}
	return accepted, nil
}

func AcceptPending(dir string, shas []string) error {
	accepted, err := AcceptedCommits(dir)
	if err != nil {
		return err
	}
	for _, sha := range shas {
		if !shaPattern.MatchString(sha) {
			return errors.New("not a commit id: " + sha)
		}
		accepted[sha] = true
	}
	merged := make([]string, 0, len(accepted))
	for sha := range accepted {
		merged = append(merged, sha)
	}
	sort.Strings(merged)
	path, err := batchFile(dir, acceptedFile)
	if err != nil {
		return err
	}
	if err := writeSHAs(path, merged); err != nil {
		return err
	}
	return SavePending(dir, nil)
}

func DescribeCommits(dir string, shas []string) (string, error) {
	args := append([]string{"log", "--no-walk=unsorted", "--format=%h  %an <%ae>  %s"}, shas...)
	return git(dir, args...)
}
