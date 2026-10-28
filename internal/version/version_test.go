package version

import "testing"

func TestCurrentUsesLinkedVersion(t *testing.T) {
	old := Version
	t.Cleanup(func() { Version = old })
	for in, want := range map[string]string{"1.2.3": "v1.2.3", "v0.1.0": "v0.1.0", " v2.0.0\n": "v2.0.0"} {
		Version = in
		if got := Current(); got != want {
			t.Errorf("Version %q: got %q, want %q", in, got, want)
		}
	}
}

func TestCurrentWithoutLinkedVersionIsDev(t *testing.T) {
	old := Version
	t.Cleanup(func() { Version = old })
	Version = ""
	if got := Current(); got != Dev {
		t.Errorf("got %q, want %q", got, Dev)
	}
}
