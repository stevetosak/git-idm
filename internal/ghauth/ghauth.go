package ghauth

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/stevetosak/git-idm/internal/config"
)

var (
	ErrUnauthorized = errors.New("the token is invalid or revoked")
	ErrUnreachable  = errors.New("GitHub could not be reached")
)

var Timeout = 8 * time.Second

type Account struct {
	Login  string   `json:"login"`
	ID     int64    `json:"id"`
	Scopes []string `json:"-"`
}

func (a Account) CanAddKeys() bool {
	return a.Scopes == nil || slices.Contains(a.Scopes, "admin:public_key") || slices.Contains(a.Scopes, "write:public_key")
}

func (a Account) CanAddSigningKeys() bool {
	return a.Scopes == nil || slices.Contains(a.Scopes, "admin:ssh_signing_key") || slices.Contains(a.Scopes, "write:ssh_signing_key")
}

func Fetch(gh, dir string) (Account, error) {
	ctx, cancel := context.WithTimeout(context.Background(), Timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, gh, "api", "user", "--include")
	cmd.Env = append(os.Environ(), "GH_CONFIG_DIR="+config.ExpandHome(dir))
	cmd.WaitDelay = time.Second
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if ctx.Err() != nil {
		return Account{}, fmt.Errorf("%w (no answer in %s)", ErrUnreachable, Timeout)
	}
	if err != nil {
		return Account{}, classify(string(out), stderr.String(), err)
	}
	return Parse(string(out))
}

var unreachableSigns = []string{"error connecting", "dial tcp", "no such host", "i/o timeout", "connection refused", "network is unreachable"}

func classify(out, stderr string, err error) error {
	status := statusCode(out)
	if status == 401 || strings.Contains(stderr, "HTTP 401") || strings.Contains(stderr, "gh auth login") {
		return ErrUnauthorized
	}
	if status == 0 {
		for _, sign := range unreachableSigns {
			if strings.Contains(stderr, sign) {
				return fmt.Errorf("%w: %s", ErrUnreachable, firstLine(stderr))
			}
		}
	}
	return fmt.Errorf("%v: %s", err, strings.TrimSpace(stderr))
}

func statusCode(response string) int {
	fields := strings.Fields(firstLine(response))
	if len(fields) < 2 || !strings.HasPrefix(fields[0], "HTTP/") {
		return 0
	}
	code, _ := strconv.Atoi(fields[1])
	return code
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(s), "\n")
	return strings.TrimSpace(line)
}

func Parse(response string) (Account, error) {
	response = strings.ReplaceAll(response, "\r\n", "\n")
	headers, body, found := strings.Cut(response, "\n\n")
	if !found {
		headers, body = "", response
	}
	var a Account
	if err := json.Unmarshal([]byte(body), &a); err != nil || a.Login == "" {
		return Account{}, fmt.Errorf("read GitHub account: unexpected answer from gh api user")
	}
	for _, line := range strings.Split(headers, "\n") {
		name, value, ok := strings.Cut(line, ":")
		if !ok || !strings.EqualFold(strings.TrimSpace(name), "X-OAuth-Scopes") {
			continue
		}
		a.Scopes = []string{}
		for _, scope := range strings.Split(value, ",") {
			if scope = strings.TrimSpace(scope); scope != "" {
				a.Scopes = append(a.Scopes, scope)
			}
		}
	}
	return a, nil
}
