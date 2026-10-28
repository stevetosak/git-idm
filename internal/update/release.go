package update

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode"

	"github.com/stevetosak/git-idm/internal/version"
)

const (
	Repo        = "stevetosak/git-idm"
	InstallLine = "curl -fsSL https://raw.githubusercontent.com/" + Repo + "/main/install.sh | sh"
)

var ErrNoRelease = errors.New("no release is published yet")

type Client struct {
	API      string
	Download string
	HTTP     *http.Client
}

type Release struct {
	Tag   string `json:"tag_name"`
	Notes string `json:"body"`
	URL   string `json:"html_url"`
}

func NewClient() *Client {
	return &Client{
		API:      "https://api.github.com",
		Download: "https://github.com/" + Repo + "/releases/download",
		HTTP:     &http.Client{Timeout: 60 * time.Second},
	}
}

func (c *Client) Latest(ctx context.Context) (Release, error) {
	body, err := c.get(ctx, c.API+"/repos/"+Repo+"/releases/latest", 1<<20, "application/vnd.github+json")
	if err != nil {
		return Release{}, err
	}
	var r Release
	if err := json.Unmarshal(body, &r); err != nil {
		return Release{}, fmt.Errorf("read the latest release: %w", err)
	}
	if !ValidTag(r.Tag) {
		return Release{}, fmt.Errorf("the latest release has an unexpected tag %q", clean(r.Tag))
	}
	if !strings.HasPrefix(r.URL, "https://github.com/"+Repo+"/") {
		r.URL = "https://github.com/" + Repo + "/releases/tag/" + r.Tag
	}
	return r, nil
}

func (c *Client) get(ctx context.Context, url string, limit int64, accept string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "git-idm/"+version.Current())
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusNotFound && strings.Contains(url, "/releases/latest"):
		return nil, ErrNoRelease
	case resp.StatusCode != http.StatusOK:
		return nil, fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > limit {
		return nil, fmt.Errorf("GET %s: the answer is larger than %d bytes", url, limit)
	}
	return body, nil
}

var (
	listPrefix   = regexp.MustCompile(`^[*-]\s+`)
	commitPrefix = regexp.MustCompile(`^[0-9a-f]{7,40}\s+`)
	branchPrefix = regexp.MustCompile(`^[\w.-]+ \| `)
	ansiEscape   = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]`)
)

const maxNoteWidth = 100

func Summary(notes string, max int) (lines []string, more int) {
	for _, line := range strings.Split(notes, "\n") {
		line = strings.TrimSpace(clean(line))
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "**Full Changelog**") {
			continue
		}
		line = branchPrefix.ReplaceAllString(commitPrefix.ReplaceAllString(listPrefix.ReplaceAllString(line, ""), ""), "")
		if len(lines) == max {
			more++
			continue
		}
		lines = append(lines, truncate(line, maxNoteWidth))
	}
	return lines, more
}

func clean(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '\t' {
			return ' '
		}
		if unicode.IsControl(r) || unicode.Is(unicode.Bidi_Control, r) {
			return -1
		}
		return r
	}, ansiEscape.ReplaceAllString(s, ""))
}

func truncate(s string, width int) string {
	r := []rune(s)
	if len(r) <= width {
		return s
	}
	return string(r[:width-1]) + "…"
}
