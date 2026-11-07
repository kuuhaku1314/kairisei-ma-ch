package game

import (
	"encoding/json"
	"fmt"
	"testing"
)

func battleCatalogFixture(t testing.TB, raw json.RawMessage) *battleCatalogState {
	t.Helper()
	state, err := newBattleCatalogState(raw, nil)
	if err != nil {
		t.Fatal(err)
	}
	return state
}

func battleDefinitionsFixture(t testing.TB, raw json.RawMessage) *TeamBattleDefinitions {
	t.Helper()
	definitions, err := NewTeamBattleDefinitions(raw)
	if err != nil {
		t.Fatal(err)
	}
	return definitions
}

func TestSharedBattleDefinitionsKeepPrivateProgressAndViews(t *testing.T) {
	raw := json.RawMessage(`{"9":[],"10":[{"0":300101,"9":0,"10":[{"0":30010102,"5":15,"6":8,"7":1,"10":0,"16":0,"entry_limit":[{"slot1_arthur_type":1}],"future":{"values":[9007199254740993]}},{"0":30010103,"10":-1,"16":0}]}],"11":[],"12":[]}`)
	definitions := battleDefinitionsFixture(t, raw)
	first, err := newBattleCatalogState(raw, definitions)
	if err != nil {
		t.Fatal(err)
	}
	second, err := newBattleCatalogState(raw, definitions)
	if err != nil {
		t.Fatal(err)
	}
	first.markClear(30010102, 0)
	view, err := first.view()
	if err != nil {
		t.Fatal(err)
	}
	_, boss, _ := view.findBoss(30010102)
	boss.fields["future"].(map[string]any)["values"].([]any)[0] = json.Number("7")
	other, err := second.view()
	if err != nil {
		t.Fatal(err)
	}
	_, untouched, _ := other.findBoss(30010102)
	if untouched.fields["10"] != json.Number("0") || untouched.fields["future"].(map[string]any)["values"].([]any)[0] != json.Number("9007199254740993") {
		t.Fatal("request or progress changed a shared definition")
	}
	if other.BossAccessible(30010103, 1) {
		t.Fatal("unavailable progress became accessible")
	}
	rules, _ := definitions.entryRules(30010102)
	rules.EntryLimit[0].Slot1ArthurType = 4
	rules, _ = definitions.entryRules(30010102)
	if rules.EntryLimit[0].Slot1ArthurType != 1 {
		t.Fatal("entry-rule caller changed shared slots")
	}
	account := &Account{teamBattleSolo: first}
	replacement := battleDefinitionsFixture(t, json.RawMessage(`{"9":[],"10":[{"0":300101,"9":0,"10":[{"0":30010102,"5":99,"10":0,"16":0}]}],"11":[],"12":[]}`))
	account.ApplyBattleCatalogConfiguration(replacement, nil)
	if clear, err := first.firstClear(30010102, 0); err != nil || clear {
		t.Fatal("publication lost first clear", err)
	}
	if rules, _ := first.entryRules(30010102); rules.BPUse != 99 {
		t.Fatal("publication did not replace definitions")
	}
	if rules, _ := second.entryRules(30010102); rules.BPUse != 15 {
		t.Fatal("publication mutated an older snapshot")
	}
}

func BenchmarkBattleCatalogView(b *testing.B) {
	groups := make([]any, 500)
	for i := range groups {
		bosses := make([]any, 4)
		for j := range bosses {
			bosses[j] = map[string]any{"0": 30000000 + i*4 + j, "1": 0, "4": "difficulty", "5": 15, "6": 8, "7": 1, "10": 0, "14": 1, "16": 0, "24": 0, "26": 0}
		}
		groups[i] = map[string]any{"0": 300000 + i, "1": 0, "4": fmt.Sprint("boss ", i), "9": 0, "10": bosses}
	}
	raw, err := json.Marshal(map[string]any{"9": []any{}, "10": groups, "11": []any{}, "12": []any{}})
	if err != nil {
		b.Fatal(err)
	}
	state := battleCatalogFixture(b, raw)
	for _, typed := range []bool{false, true} {
		name := "decode_each_request"
		if typed {
			name = "shared_definitions"
		}
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				var view *TeamBattleCatalog
				var err error
				if typed {
					view, err = state.view()
				} else {
					view, err = decodeTeamBattleCatalog(raw)
				}
				if err != nil {
					b.Fatal(err)
				}
				if err := view.project(nil, nil, false, false); err != nil {
					b.Fatal(err)
				}
				if !view.HasBoss(30001999) {
					b.Fatal("missing final boss")
				}
			}
		})
	}
}
