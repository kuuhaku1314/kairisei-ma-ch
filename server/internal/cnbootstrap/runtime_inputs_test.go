package cnbootstrap

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRuntimeInputsUseContainedManifestEntrypoints(t *testing.T) {
	root := filepath.Join(t.TempDir(), "资源目录")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "input"), []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	entries := map[string]string{}
	for _, key := range strings.Fields("cn-save-seed cn-asset-map cn-cpk-root cn-image-root cn-cpk-aliases cn-gacha-banner cn-five-star-gacha-banner cn-home-banner cn-card-master cn-explore-master cn-story-master cn-battle-master cn-navi-master cn-item-master cn-avatar-master cn-stamp-master cn-honor-master cn-pvp-master cn-player-progression cn-login-bonus cn-combat-card-master cn-combat-master-root") {
		entries[key] = "input"
	}
	manifest := map[string]any{"entrypoints": entries}
	filename := filepath.Join(root, "resource-set.json")
	load := func() (RuntimeInputs, error) {
		t.Helper()
		payload, err := json.Marshal(manifest)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filename, payload, 0600); err != nil {
			t.Fatal(err)
		}
		return LoadRuntimeInputs(filename)
	}
	inputs, err := load()
	want := filepath.Join(root, "input")
	if err != nil || inputs.SeedPath != want || inputs.Resources.Banners["five_star_ticket"] != want || inputs.Masters.Battle != want || inputs.CombatMasterRoot != want {
		t.Fatalf("runtime inputs: %+v, %v", inputs, err)
	}
	for _, path := range []string{"", "../input", "/input", "C:/input", "nested/../input", "nested\\input", "missing"} {
		entries["cn-save-seed"] = path
		inputs, err := load()
		if err == nil || inputs.SeedPath != "" || !strings.Contains(err.Error(), "cn-save-seed") {
			t.Fatalf("accepted invalid entrypoint %q: %+v, %v", path, inputs, err)
		}
	}
	entries["cn-save-seed"] = "input"
	delete(entries, "cn-five-star-gacha-banner")
	if _, err := load(); err == nil || !strings.Contains(err.Error(), "cn-five-star-gacha-banner") {
		t.Fatalf("missing immutable input accepted: %v", err)
	}
}
