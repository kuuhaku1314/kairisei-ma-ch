package admin

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"kairisei.local/server/internal/gamestate"
	"kairisei.local/server/internal/masterdata"
	"kairisei.local/server/internal/multiplayer"
)

func TestModernCardPresentationUsesNativeColumnsAndKeepsFirstRow(t *testing.T) {
	root := t.TempDir()
	row := make([]string, 120)
	row[0], row[4], row[5], row[7] = "10001", "称号", "名称", "MILLIONRARE"
	row[22], row[34], row[36], row[50] = "999", "2", "100", "10001234"
	content := []byte("\ufeff" + strings.Join(row, ",") + "\n")
	cardPath := filepath.Join(root, "cn602-card-master", "card.csv")
	if err := os.MkdirAll(filepath.Dir(cardPath), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cardPath, content, 0600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(content)
	source, err := json.Marshal(map[string]any{"files": []map[string]any{{"path": "cn602-card-master/card.csv", "bytes": len(content), "sha256": hex.EncodeToString(sum[:])}}})
	if err != nil {
		t.Fatal(err)
	}
	assetMap := filepath.Join(root, "asset-map.json")
	if err := os.WriteFile(assetMap, []byte(`{"catalog_assets":[{"name":"chr10_10001234"},{"name":"chr20_10001234"}]}`), 0600); err != nil {
		t.Fatal(err)
	}
	master := masterdata.CardRuntimeMaster{Source: source, CardTemplates: []gamestate.Card{{CardID: 10001}}}
	resources, err := loadAdminCardResources(master, filepath.Join(root, "runtime.json"), assetMap)
	if err != nil || len(resources) != 1 || resources[10001].PictID != 10001234 || !resources[10001].Ready {
		t.Fatalf("modern first row or PictID: %v %v", resources, err)
	}
	if err := os.WriteFile(filepath.Join(root, "skill_player.csv"), []byte("123"+strings.Repeat(",", 49)+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	details, err := multiplayer.LoadOperationsCardCatalog(cardPath, root)
	if err != nil || details[10001].LimitCount != 2 || details[10001].Prefix != "称号" {
		t.Fatalf("modern limit count: %v %v", details, err)
	}
}

func TestAdminPictureIdentitySharesOneImageAcrossCardsAndMaterials(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "card-picture"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "card-picture", "10001234.webp"), []byte("webp"), 0600); err != nil {
		t.Fatal(err)
	}
	manifest := adminAssetManifest{SchemaVersion: 2, ClientProfile: "cn602-bootstrap", State: "PASS", Purpose: "loopback_admin_web_thumbnail_only", CardImageIdentity: "picture_id", CatalogImageCoverage: map[string]adminCatalogImageCoverage{
		"card": {EntryCount: 2, ResolvedSourceCount: 2}, "material": {EntryCount: 1, ResolvedSourceCount: 1}, "item": {}, "sphere": {}, "buddy": {},
	}}
	write := func() {
		raw, err := json.Marshal(manifest)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "manifest.json"), raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
	write()
	catalog := []AdminCatalogEntry{{Kind: "card", RewardTypeID: 11, ImageURL: "/assets/card/11.webp"}, {Kind: "card", RewardTypeID: 12, ImageURL: "/assets/card/12.webp"}, {Kind: "material", RewardTypeID: 13, ImageURL: "/assets/card/13.webp"}}
	resources := map[int]adminCardResource{11: {PictID: 10001234, Ready: true}, 12: {PictID: 10001234, Ready: true}, 13: {PictID: 10001234, Ready: true}}
	if _, err := applyAdminCatalogAssetCoverage(catalog, root); err == nil {
		t.Fatal("picture identity accepted without source")
	}
	resolved, err := applyAdminCatalogAssetCoverage(catalog, root, resources)
	if err != nil || len(resolved) != 3 {
		t.Fatalf("shared image: %v %v", resolved, err)
	}
	for _, entry := range resolved {
		if entry.ImageURL != "/assets/card-picture/10001234.webp" {
			t.Fatalf("unexpected image: %+v", entry)
		}
	}
	manifest.CardImageIdentity = "unreviewed"
	write()
	if _, err := applyAdminCatalogAssetCoverage(catalog, root, resources); err == nil {
		t.Fatal("unknown image identity accepted")
	}
}
