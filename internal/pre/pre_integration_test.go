package pre

import (
	"testing"
)

// TestCheckAllIntegration runs CheckAll() and verifies each dependency
// returns a valid Status struct.
func TestCheckAllIntegration(t *testing.T) {
	statuses := CheckAll()
	if len(statuses) != 3 {
		t.Fatalf("expected 3 statuses, got %d", len(statuses))
	}
	for i, st := range statuses {
		name := All()[i].Name
		if !st.Installed && st.Path != "" {
			t.Fatalf("%s: !Installed but Path is not empty", name)
		}
		if st.Installed && st.Path == "" {
			t.Fatalf("%s: Installed but Path is empty", name)
		}
	}
}

// TestCheckFilteredNode verifies node/npm check works.
func TestCheckFilteredNode(t *testing.T) {
	statuses := CheckFiltered("node")
	if len(statuses) != 1 {
		t.Fatalf("expected 1 status for node, got %d", len(statuses))
	}
}

// TestCheckFilteredPython verifies python/pip check works.
func TestCheckFilteredPython(t *testing.T) {
	statuses := CheckFiltered("python")
	if len(statuses) != 1 {
		t.Fatalf("expected 1 status for python, got %d", len(statuses))
	}
}

// TestCheckFilteredGit verifies git check works.
func TestCheckFilteredGit(t *testing.T) {
	statuses := CheckFiltered("git")
	if len(statuses) != 1 {
		t.Fatalf("expected 1 status for git, got %d", len(statuses))
	}
}

// TestInstallMissingAll returns a result without error (may install or skip).
func TestInstallMissingAll(t *testing.T) {
	// This exercises the full InstallMissing logic. It should not panic.
	// Note: Install() may fail on this machine if winget is not available;
	// that is expected and acceptable — the function should handle it gracefully.
	ok, fail := InstallMissing("all")
	if ok+fail != 3 {
		t.Fatalf("expected 3 total (ok+fail), got ok=%d, fail=%d", ok, fail)
	}
}

// TestInstallMissingUnknown returns (0, 0) for unknown tool names.
func TestInstallMissingUnknown(t *testing.T) {
	ok, fail := InstallMissing("ruby")
	if ok != 0 || fail != 0 {
		t.Fatalf("expected (0,0) for unknown tool, got (%d,%d)", ok, fail)
	}
}
