package cnbootstrap

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// There is deliberately no patch/version.dat, old Menu bundle or asset map in
// this fixture. Both startup and CDN export must consume the selected profiles.
func TestPlatformOnlyResourcesAndCDN(t *testing.T) {
	root := t.TempDir()
	profiles := []string{
		writePlatformFixture(t, root, "StandaloneWindows64", []byte("windows")),
		writePlatformFixture(t, root, "Android", []byte("android")),
	}
	cpkRoot := filepath.Join(root, "cpk")
	if err := os.Mkdir(cpkRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cpkRoot, "cv_navi_5.cpk"), []byte("voice"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cpkRoot, "versions.json"), []byte(`{"schema_version":1,"versions":[{"name":"cv_navi_5.cpk","version":3}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	aliases := filepath.Join(root, "aliases.json")
	if err := os.WriteFile(aliases, []byte(`{"schema_version":2,"client_profile":"cn602-bootstrap","aliases":[{"alias_cpk_name":"cv_navi_94.cpk","source_cpk_name":"cv_navi_5.cpk"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	images := writeTestCardImages(t, root)
	for _, name := range cn602GachaBannerFiles {
		if err := os.WriteFile(filepath.Join(root, name), []byte("banner"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	resources, err := loadClientResources(ResourcesConfig{
		PlatformDeliveries: profiles, CPKRoot: cpkRoot, CPKAliases: aliases, ImageRoot: images,
		Banners: map[string]string{"": aliases, "five_star_ticket": aliases, "home": aliases},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(resources.AvailableBundles) != 1 {
		t.Fatal("modern startup did not use its validated target inventory")
	}
	if _, exists := resources.AvailableBundles["main_c/menu.dat"]; !exists {
		t.Fatal("shared business inventory lost the modern owner")
	}
	files := []map[string]any{}
	for _, name := range []string{"cpk/versions.json", "cpk/cv_navi_5.cpk", "image/manifest.json", "image/chr51/chr51_10000010.png"} {
		content, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(name)))
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, map[string]any{"path": name, "bytes": len(content), "sha256": fmt.Sprintf("%x", sha256.Sum256(content))})
	}
	manifest, _ := json.Marshal(map[string]any{"schema_version": 1, "client_profile": "cn602-bootstrap", "path_base": "RESOURCE_SET_ROOT", "files": files,
		"entrypoints": map[string]string{"cn-cpk-root": "cpk", "cn-cpk-aliases": filepath.Base(aliases), "cn-image-root": "image"}})
	manifestPath := filepath.Join(root, "resource-set.json")
	if err := os.WriteFile(manifestPath, manifest, 0o600); err != nil {
		t.Fatal(err)
	}
	exported, err := BuildCDNManifest(manifestPath, profiles...)
	if err != nil || exported.SchemaVersion != 2 || len(exported.PlatformContents) != 2 || len(exported.Files) != 7 {
		t.Fatalf("profile-only CDN export: %v, %v", exported, err)
	}
	var voiceSources []string
	for _, file := range exported.Files {
		if strings.HasPrefix(file.Key, "cpk/CPK/cv_navi_") {
			voiceSources = append(voiceSources, file.SourcePath+file.SHA256)
		}
		if strings.HasPrefix(file.Key, "patch/Android/") {
			t.Fatal("modern export included an unused Android carrier")
		}
	}
	if len(voiceSources) != 2 || voiceSources[0] != voiceSources[1] {
		t.Fatal("CPK alias lost its physical source identity")
	}
	if _, err := BuildCDNManifest(manifestPath); err == nil {
		t.Fatal("CDN export without target profiles was accepted")
	}
}
