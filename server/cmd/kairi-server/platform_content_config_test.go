package main

import (
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestPlatformContentConfigUsesSelectedPackagePaths(t *testing.T) {
	root := t.TempDir()
	pc, android := "platforms/pc/delivery.json", "platforms/android/delivery.json"
	for _, name := range []string{pc, android} {
		p := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("{}"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	config := filepath.Join(root, "deployment.json")
	for _, selected := range [][]string{{pc, android}, {pc}} {
		body, _ := json.Marshal(map[string]any{"platform_contents": selected, "other_deployment_metadata": true})
		if err := os.WriteFile(config, body, 0o600); err != nil {
			t.Fatal(err)
		}
		profiles, err := loadPlatformContentConfig(config)
		if err != nil {
			t.Fatal(err)
		}
		want := make(stringListFlag, 0, len(selected))
		for _, name := range selected {
			p, err := filepath.EvalSymlinks(filepath.Join(root, filepath.FromSlash(name)))
			if err != nil {
				t.Fatal(err)
			}
			want = append(want, p)
		}
		if !reflect.DeepEqual(profiles, want) {
			t.Fatalf("profile selection follows deployment array: %v, want %v", profiles, want)
		}
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	if err := run([]string{"-platform-content-config", config, "-platform-content", pc}, logger); err == nil || !strings.Contains(err.Error(), "cannot be combined") {
		t.Fatalf("ambiguous config/direct flags: %v", err)
	}
}

func TestPlatformContentConfigRejectsInvalidPackagePaths(t *testing.T) {
	config := filepath.Join(t.TempDir(), "deployment.json")
	for _, body := range []string{
		`{}`, `{"platform_contents":[]}`, `{"platform_contents":"not-an-array"}`,
		`{"platform_contents":["../outside.json"]}`, `{"platform_contents":["/outside.json"]}`,
		`{"platform_contents":["C:/outside.json"]}`, `{"platform_contents":["platforms/../../outside.json"]}`,
	} {
		if err := os.WriteFile(config, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := loadPlatformContentConfig(config); err == nil {
			t.Fatalf("invalid deployment selection accepted: %s", body)
		}
	}
}

func TestPlatformContentConfigRejectsLinkedEscape(t *testing.T) {
	root, outside := t.TempDir(), filepath.Join(t.TempDir(), "outside.json")
	if err := os.WriteFile(outside, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "profile.json")); err != nil {
		t.Skipf("host cannot create the symlink fixture: %v", err)
	}
	config := filepath.Join(root, "deployment.json")
	if err := os.WriteFile(config, []byte(`{"platform_contents":["profile.json"]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadPlatformContentConfig(config); err == nil || !strings.Contains(err.Error(), "escapes package root") {
		t.Fatalf("linked profile escaped deployment root: %v", err)
	}
}
