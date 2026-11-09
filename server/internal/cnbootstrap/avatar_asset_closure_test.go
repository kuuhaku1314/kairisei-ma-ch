package cnbootstrap

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadCNAssetMapDeliveryCRC32(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	version := []byte("test version")
	digest := sha256.Sum256(version)
	manifest := map[string]any{
		"schema_version": 2,
		"client_profile": "cn602-bootstrap",
		"source": map[string]any{
			"catalog_version":                    cn602CatalogVersion,
			"version_dat_sha256":                 fmt.Sprintf("%x", digest),
			"parsed_unity_bundle_count":          1,
			"non_unity_bundle_count":             0,
			"bundle_dependency_edge_count":       0,
			"unresolved_bundle_dependency_count": 0,
			"scrambled_bundle_count":             1,
			"plain_bundle_count":                 0,
		},
		"bundles": []map[string]any{{
			"bundle":         cn602MenuBundle,
			"cab_name":       "cab-test",
			"scrambled":      true,
			"delivery_crc32": "143c9c4d",
			"dependencies":   []string{},
		}},
		"catalog_assets": []map[string]any{{
			"directory": "scenes",
			"name":      "Menu",
			"base_dir":  "Assets/Plugins/rhyme/EeL/Resources/",
			"extension": ".prefab",
			"bundle":    cn602MenuBundle,
		}},
	}
	contents, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "asset-map.json")
	if err := os.WriteFile(path, contents, 0o600); err != nil {
		t.Fatal(err)
	}
	assets, metadata, err := loadCNAssetMap(
		path,
		fmt.Sprintf("%x", digest),
		map[string]struct{}{cn602MenuBundle: {}},
	)
	if err != nil {
		t.Fatal(err)
	}
	if got := metadata[cn602MenuBundle].deliveryCRC32; got != "143C9C4D" {
		t.Fatalf("delivery CRC = %q, want 143C9C4D", got)
	}
	if len(assets) != 1 || assets[0].baseDir != "Assets/Plugins/rhyme/EeL/Resources/" {
		t.Fatalf("catalog base directory = %#v, want preserved nonstandard base directory", assets)
	}
	manifest["source"].(map[string]any)["version_dat_sha256"] = "0000000000000000000000000000000000000000000000000000000000000000"
	contents, err = json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	invalidIdentityPath := filepath.Join(root, "asset-map-wrong-version.json")
	if err := os.WriteFile(invalidIdentityPath, contents, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := loadCNAssetMap(
		invalidIdentityPath,
		fmt.Sprintf("%x", digest),
		map[string]struct{}{cn602MenuBundle: {}},
	); err == nil {
		t.Fatal("asset map for a different version.dat was accepted")
	}
}
