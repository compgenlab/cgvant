// Package software preflight-checks that the external executables a tool or
// source declares (via `requires`) are present on PATH, so a missing dependency
// (apptainer, python3, bgzip, …) fails fast with a clear message instead of a
// cryptic "executable file not found" partway through a run.
package software

import (
	"fmt"
	"os/exec"
	"strings"
)

// Missing returns the subset of names not found on PATH (via exec.LookPath).
// Empty/blank names are ignored. Order and duplicates from the input are
// preserved for the missing entries.
func Missing(names []string) []string {
	var missing []string
	for _, n := range names {
		if strings.TrimSpace(n) == "" {
			continue
		}
		if _, err := exec.LookPath(n); err != nil {
			missing = append(missing, n)
		}
	}
	return missing
}

// Check returns an error naming any required executables missing from PATH,
// prefixed by label (e.g. a tool/source ID). Returns nil when all are present.
func Check(label string, names []string) error {
	missing := Missing(names)
	if len(missing) == 0 {
		return nil
	}
	return fmt.Errorf("%s: required software not found on PATH: %s", label, strings.Join(missing, ", "))
}
