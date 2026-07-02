package software

import (
	"strings"
	"testing"
)

func TestMissing(t *testing.T) {
	// "sh" is on PATH in any POSIX test environment; the bogus name is not.
	got := Missing([]string{"sh", "", "  ", "definitely-not-a-real-binary-xyz"})
	if len(got) != 1 || got[0] != "definitely-not-a-real-binary-xyz" {
		t.Fatalf("Missing = %v, want [definitely-not-a-real-binary-xyz]", got)
	}
}

func TestMissingAllPresent(t *testing.T) {
	if got := Missing([]string{"sh", ""}); got != nil {
		t.Fatalf("Missing = %v, want nil", got)
	}
}

func TestCheck(t *testing.T) {
	if err := Check("x", []string{"sh"}); err != nil {
		t.Fatalf("Check(present) = %v, want nil", err)
	}
	err := Check("vep:112", []string{"definitely-not-a-real-binary-xyz"})
	if err == nil {
		t.Fatal("Check(missing) = nil, want error")
	}
	if msg := err.Error(); !strings.Contains(msg, "vep:112") || !strings.Contains(msg, "definitely-not-a-real-binary-xyz") {
		t.Fatalf("Check error = %q, want it to name the label and the missing binary", msg)
	}
}
