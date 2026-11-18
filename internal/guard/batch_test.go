package guard

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBatchAcceptMovesPendingToAccepted(t *testing.T) {
	_, repo := pushRepo(t)
	first := commit(t, repo, "a@x.example", "a@x.example")
	second := commit(t, repo, "b@x.example", "b@x.example")

	if got, err := Pending(repo); err != nil || len(got) != 0 {
		t.Fatalf("no pending file: %v %v", got, err)
	}
	if err := SavePending(repo, []string{first, second}); err != nil {
		t.Fatal(err)
	}
	pendingPath := filepath.Join(repo, ".git", "git-idm", "pending")
	data, _ := os.ReadFile(pendingPath)
	os.WriteFile(pendingPath, append(data, []byte("not-a-sha\n; rm -rf /\n")...), 0o600)

	sub := filepath.Join(repo, "sub")
	os.MkdirAll(sub, 0o755)
	pending, err := Pending(sub)
	if err != nil || len(pending) != 2 || pending[0] != first || pending[1] != second {
		t.Fatalf("pending from a subfolder = %v %v", pending, err)
	}
	if err := AcceptPending(sub, pending); err != nil {
		t.Fatal(err)
	}
	accepted, err := AcceptedCommits(repo)
	if err != nil || !accepted[first] || !accepted[second] || len(accepted) != 2 {
		t.Fatalf("accepted = %v %v", accepted, err)
	}
	if _, err := os.Stat(pendingPath); !os.IsNotExist(err) {
		t.Errorf("pending must be removed after accept: %v", err)
	}
	if err := AcceptPending(repo, []string{"HEAD"}); err == nil {
		t.Error("accept must refuse a value that is not a full commit id")
	}
}
