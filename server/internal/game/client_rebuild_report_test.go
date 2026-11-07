package game

import (
	"os"
	"path/filepath"
	"testing"
)

// Exercise the actual settlement parser with reports produced through the
// compiled C# / shared-library seam, rather than invented START-only fixtures.
func TestClientRebuildInputReports(t *testing.T) {
	input := os.Getenv("CN_REBUILT_BATTLE_REPORTS")
	if input == "" {
		t.Skip("set CN_REBUILT_BATTLE_REPORTS to captured report directories")
	}
	for _, root := range filepath.SplitList(input) {
		paths, err := filepath.Glob(filepath.Join(root, "*", "final-report.csv"))
		if err != nil || len(paths) == 0 {
			t.Fatalf("missing captured reports in %s: %v", root, err)
		}
		for _, path := range paths {
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if err = ValidateNativeBattleCommand(string(data), true); err != nil {
				t.Fatalf("%s: %v", path, err)
			}
		}
	}
}
