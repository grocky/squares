package version

import "testing"

func TestGet_WithLdflagCommit(t *testing.T) {
	// Save and restore original value
	orig := commit
	defer func() { commit = orig }()

	commit = "abc1234"
	got := Get()
	if got != "abc1234" {
		t.Errorf("Get() = %q, want %q", got, "abc1234")
	}
}

func TestGet_WithoutLdflagCommit(t *testing.T) {
	orig := commit
	defer func() { commit = orig }()

	commit = ""
	got := Get()
	// Without ldflag, it falls back to VCS info or "unavailable".
	// In test context there's no VCS info, so it should be non-empty.
	if got == "" {
		t.Error("Get() returned empty string, expected some value")
	}
}
