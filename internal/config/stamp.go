package config

import (
	"os"
	"path/filepath"
	"strconv"
	"time"
)

const stampFile = "changed"

func StampPath(dir string) string {
	return filepath.Join(dir, stampFile)
}

func DefaultStampPath() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return StampPath(dir), nil
}

func Touch(dir string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	stamp := strconv.FormatInt(time.Now().UnixNano(), 10) + "\n"
	return os.WriteFile(StampPath(dir), []byte(stamp), 0o600)
}
