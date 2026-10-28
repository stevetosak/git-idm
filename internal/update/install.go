package update

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const maxArchive = 100 << 20

var ErrNoWriteAccess = errors.New("no write access")

func AssetName() string {
	return fmt.Sprintf("git-idm_%s_%s.tar.gz", runtime.GOOS, runtime.GOARCH)
}

func (c *Client) Install(ctx context.Context, tag, exe string) error {
	if !ValidTag(tag) {
		return fmt.Errorf("invalid release tag %q", clean(tag))
	}
	base := c.Download + "/" + tag + "/"
	asset := AssetName()
	sums, err := c.get(ctx, base+"checksums.txt", 64<<10, "")
	if err != nil {
		return err
	}
	want, err := checksumFor(string(sums), asset)
	if err != nil {
		return err
	}
	archive, err := c.get(ctx, base+asset, maxArchive, "")
	if err != nil {
		return err
	}
	got := sha256.Sum256(archive)
	if hex.EncodeToString(got[:]) != want {
		return fmt.Errorf("checksum mismatch for %s, the download is not safe to use", asset)
	}
	binary, err := extractBinary(archive)
	if err != nil {
		return err
	}
	return replace(ctx, exe, binary, tag)
}

func checksumFor(sums, asset string) (string, error) {
	for _, line := range strings.Split(sums, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[1] == asset && len(fields[0]) == 64 {
			if _, err := hex.DecodeString(fields[0]); err == nil {
				return strings.ToLower(fields[0]), nil
			}
		}
	}
	return "", fmt.Errorf("checksums.txt has no entry for %s", asset)
}

func extractBinary(archive []byte) ([]byte, error) {
	gz, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return nil, fmt.Errorf("read the archive: %w", err)
	}
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return nil, fmt.Errorf("the archive has no git-idm binary")
		}
		if err != nil {
			return nil, fmt.Errorf("read the archive: %w", err)
		}
		if h.Name != "git-idm" || h.Typeflag != tar.TypeReg {
			continue
		}
		data, err := io.ReadAll(io.LimitReader(tr, maxArchive+1))
		if err != nil {
			return nil, err
		}
		if len(data) > maxArchive {
			return nil, fmt.Errorf("the git-idm binary in the archive is too large")
		}
		return data, nil
	}
}

func replace(ctx context.Context, exe string, binary []byte, tag string) error {
	dir := filepath.Dir(exe)
	tmp, err := os.CreateTemp(dir, ".git-idm-update-*")
	if err != nil {
		if errors.Is(err, fs.ErrPermission) {
			return fmt.Errorf("%w to %s", ErrNoWriteAccess, dir)
		}
		return err
	}
	name := tmp.Name()
	done := false
	defer func() {
		if !done {
			os.Remove(name)
		}
	}()
	_, werr := tmp.Write(binary)
	cerr := tmp.Close()
	if err := errors.Join(werr, cerr, os.Chmod(name, 0o755)); err != nil {
		return err
	}
	if err := checkBinary(ctx, name, tag); err != nil {
		return err
	}
	if err := os.Rename(name, exe); err != nil {
		return err
	}
	done = true
	return nil
}

func checkBinary(ctx context.Context, path, tag string) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, path, "--version").Output()
	if err != nil {
		return fmt.Errorf("the new binary does not run: %w", err)
	}
	if got := strings.TrimSpace(string(out)); got != "git-idm "+tag {
		return fmt.Errorf("the new binary reports %q, expected git-idm %s", clean(got), tag)
	}
	return nil
}
