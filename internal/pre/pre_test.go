package pre

import (
	"reflect"
	"testing"
)

func TestAllReturnsThree(t *testing.T) {
	deps := All()
	if len(deps) != 3 {
		t.Fatalf("expected 3 dependencies, got %d", len(deps))
	}
}

func TestDependencyNames(t *testing.T) {
	names := []string{}
	for _, d := range All() {
		names = append(names, d.Name)
	}
	expected := []string{"Node.js / npm", "Python / pip", "Git"}
	if !reflect.DeepEqual(names, expected) {
		t.Fatalf("expected %v, got %v", expected, names)
	}
}

func TestGetByTool(t *testing.T) {
	tests := []struct {
		input    string
		wantName string
		wantLen  int
	}{
		{"node", "Node.js / npm", 1},
		{"npm", "Node.js / npm", 1},
		{"NODE", "Node.js / npm", 1},
		{"python", "Python / pip", 1},
		{"pip", "Python / pip", 1},
		{"git", "Git", 1},
		{"all", "Node.js / npm", 3},
		{"unknown", "", 0},
	}
	for _, tc := range tests {
		result := GetByTool(tc.input)
		if len(result) != tc.wantLen {
			t.Fatalf("GetByTool(%q): expected len %d, got %d", tc.input, tc.wantLen, len(result))
		}
		if tc.wantLen > 0 && result[0].Name != tc.wantName {
			t.Fatalf("GetByTool(%q): expected %q, got %q", tc.input, tc.wantName, result[0].Name)
		}
	}
}

func TestGetByToolInvalid(t *testing.T) {
	if got := GetByTool("ruby"); got != nil {
		t.Fatalf("GetByTool(ruby): expected nil, got %v", got)
	}
}

func TestStatusFields(t *testing.T) {
	// Check() on a missing binary should report !Installed and empty Path/Version.
	d := Dependency{Binary: "nonexistent-binary-12345"}
	st := d.Check()
	if st.Installed {
		t.Fatalf("expected not installed")
	}
	if st.Path != "" {
		t.Fatalf("expected empty Path, got %q", st.Path)
	}
	if st.Version != "" {
		t.Fatalf("expected empty Version, got %q", st.Version)
	}
}

func TestDependencyFieldsComplete(t *testing.T) {
	for _, d := range All() {
		if d.Binary == "" {
			t.Fatalf("Dependency %q: missing Binary", d.Name)
		}
		if d.WingetPackage == "" {
			t.Fatalf("Dependency %q: missing WingetPackage", d.Name)
		}
		if d.AptPackage == "" {
			t.Fatalf("Dependency %q: missing AptPackage", d.Name)
		}
		if d.YumPackage == "" {
			t.Fatalf("Dependency %q: missing YumPackage", d.Name)
		}
		if d.BrewPackage == "" {
			t.Fatalf("Dependency %q: missing BrewPackage", d.Name)
		}
		if d.ManualInstall == "" {
			t.Fatalf("Dependency %q: missing ManualInstall", d.Name)
		}
	}
}
