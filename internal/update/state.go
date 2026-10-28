package update

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"
)

const Interval = 24 * time.Hour

type State struct {
	CheckedAt  time.Time `json:"checked_at"`
	NotifiedAt time.Time `json:"notified_at"`
	Latest     string    `json:"latest,omitempty"`
	Notes      string    `json:"notes,omitempty"`
	URL        string    `json:"url,omitempty"`
}

func LoadState(path string) State {
	var s State
	data, err := os.ReadFile(path)
	if err != nil || json.Unmarshal(data, &s) != nil || (s.Latest != "" && !ValidTag(s.Latest)) {
		return State{}
	}
	return s
}

func (s State) Save(path string) error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".update-*.json")
	if err != nil {
		return err
	}
	_, werr := tmp.Write(data)
	cerr := tmp.Close()
	if err := errors.Join(werr, cerr); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return os.Rename(tmp.Name(), path)
}

func (s State) CheckDue(now time.Time) bool {
	return now.Sub(s.CheckedAt) >= Interval
}

func (s State) NoticeDue(current string, now time.Time) bool {
	return Newer(s.Latest, current) && now.Sub(s.NotifiedAt) >= Interval
}

func (s *State) Record(r Release, now time.Time) {
	if r.Tag != s.Latest {
		s.NotifiedAt = time.Time{}
	}
	s.CheckedAt, s.Latest, s.Notes, s.URL = now, r.Tag, r.Notes, r.URL
}
