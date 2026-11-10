package admin

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"kairisei.local/server/battlecontent"
	"kairisei.local/server/internal/gamestate"
	"kairisei.local/server/internal/masterdata"
	"kairisei.local/server/internal/multiplayer"
)

// An opt-in audit of the actual complete candidate. It reads only immutable
// inputs and uses in-memory inventories; it opens no account DB or listener.
func TestModernControlProjectionCandidate(t *testing.T) {
	root, oldRoot := os.Getenv("KAIRI_MODERN_CONTROL_AUDIT_ROOT"), os.Getenv("KAIRI_MODERN_CONTROL_PREDECESSOR")
	if root == "" && oldRoot == "" {
		t.Skip("complete local candidate is not requested")
	}
	if root == "" || oldRoot == "" {
		t.Fatal("both candidate and predecessor are required")
	}
	control := filepath.Join(root, "_local", "control", "server")
	masterPath := filepath.Join(control, "cn602-card-runtime-master.json")
	cards, err := masterdata.LoadCardRuntimeMaster(masterPath)
	if err != nil {
		t.Fatal(err)
	}
	old, err := masterdata.LoadCardRuntimeMaster(filepath.Join(oldRoot, "_local", "control", "server", "cn602-card-runtime-master.json"))
	if err != nil {
		t.Fatal(err)
	}
	items, err := masterdata.LoadItemRuntimeMaster(filepath.Join(control, "cn602-item-runtime-master.json"))
	if err != nil {
		t.Fatal(err)
	}
	resources, err := loadAdminCardResources(cards, masterPath, filepath.Join(root, "asset-map.json"))
	if err != nil {
		t.Fatal(err)
	}
	catalog, _, err := BuildAdminCatalog(cards, items)
	if err != nil {
		t.Fatal(err)
	}
	catalog, err = applyAdminCatalogAssetCoverage(catalog, filepath.Join(control, "cn602-admin-assets"), resources)
	if err != nil {
		t.Fatal(err)
	}
	cardPath := filepath.Join(control, "cn602-card-master", "card.csv")
	battleRoot := filepath.Join(control, "cn602-battle-master")
	details, err := multiplayer.LoadOperationsCardCatalog(cardPath, battleRoot)
	if err != nil {
		t.Fatal(err)
	}
	seenImages := map[string]bool{}
	counts := map[string]int{}
	for _, entry := range catalog {
		counts[entry.Kind]++
		if entry.Kind == "card" || entry.Kind == "material" {
			if !strings.HasPrefix(entry.ImageURL, "/assets/card-picture/") {
				t.Fatalf("old image namespace survived: %+v", entry)
			}
			seenImages[entry.ImageURL] = true
		}
		if entry.Kind == "card" {
			if _, ok := details[entry.RewardTypeID]; !ok || !resources[entry.RewardTypeID].Ready {
				t.Fatalf("card %d has incomplete source", entry.RewardTypeID)
			}
		}
	}
	fileDefinitions, err := battlecontent.LoadDefinitions(cardPath, battleRoot)
	if err != nil {
		t.Fatal(err)
	}
	tables := map[string]string{}
	for _, directory := range []string{battleRoot, filepath.Dir(cardPath)} {
		entries, err := os.ReadDir(directory)
		if err != nil {
			t.Fatal(err)
		}
		for _, entry := range entries {
			if entry.IsDir() || filepath.Ext(entry.Name()) != ".csv" {
				continue
			}
			if _, ok := tables[entry.Name()]; ok {
				t.Fatalf("duplicate control source: %s", entry.Name())
			}
			raw, err := os.ReadFile(filepath.Join(directory, entry.Name()))
			if err != nil {
				t.Fatal(err)
			}
			tables[entry.Name()] = string(raw)
		}
	}
	memoryDefinitions, err := battlecontent.LoadDefinitionsFromTables(tables)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(fileDefinitions, memoryDefinitions) {
		t.Fatal("file and client-memory definitions differ")
	}
	var state gamestate.State
	seed, err := os.ReadFile(filepath.Join(oldRoot, "server", "config", "cn602-save-template.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(seed, &state); err != nil {
		t.Fatal(err)
	}
	state.Cards = nil
	state.ContainerCards = nil
	for i, card := range old.CardTemplates {
		card.UniqueID = int64(i + 1)
		state.Cards = append(state.Cards, card)
		card.UniqueID = int64(i + len(old.CardTemplates) + 1)
		card.Level = card.LevelMax
		card.Experience, err = gamestate.CardExperienceAtLevelStart(card.Level, card.LevelMax, old.CardExperienceTables[card.ExperienceTableID])
		if err != nil {
			t.Fatal(err)
		}
		card.Love = card.LoveMax
		card.Fame = card.FameMax
		card.IsLock = 1
		state.ContainerCards = append(state.ContainerCards, card)
	}
	if err := masterdata.ApplyCardRuntimeMaster(&state, old); err != nil {
		t.Fatal("prepare isolated old progress: ", err)
	}
	before := append(append([]gamestate.Card(nil), state.Cards...), state.ContainerCards...)
	if err := masterdata.ApplyCardRuntimeMaster(&state, cards); err != nil {
		t.Fatalf("modern definition refresh: %v", err)
	}
	after := append(append([]gamestate.Card(nil), state.Cards...), state.ContainerCards...)
	if len(before) != len(after) {
		t.Fatal("definition update changed inventory size")
	}
	for i, a := range after {
		b := before[i]
		if a.UniqueID != b.UniqueID || a.CardID != b.CardID || a.Experience != b.Experience || a.Love != b.Love || a.Fame != b.Fame || a.IsLock != b.IsLock || !reflect.DeepEqual(a.SkillLevels, b.SkillLevels) {
			t.Fatalf("mutable progress changed: %d", a.UniqueID)
		}
	}
	if err := masterdata.ApplyCardRuntimeMaster(&state, cards); err != nil {
		t.Fatalf("definition refresh: %v", err)
	}
	if !reflect.DeepEqual(after, append(append([]gamestate.Card(nil), state.Cards...), state.ContainerCards...)) {
		t.Fatal("repeated definition refresh changed inventory")
	}
	result, err := json.Marshal(map[string]any{"catalog": counts, "unique_card_images": len(seenImages), "definition_cards": len(fileDefinitions.Cards), "migration_inventory_rows": len(before), "file_memory_definitions_equal": true, "mutable_progress_preserved": true, "idempotent": true})
	if err != nil {
		t.Fatal(err)
	}
	t.Log(string(result))
}
