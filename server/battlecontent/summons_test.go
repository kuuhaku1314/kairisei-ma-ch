package battlecontent

import (
	"os"
	"reflect"
	"strings"
	"testing"

	"kairisei.local/server/battlemodel"
)

func TestSummonColumnsAndIdentity(t *testing.T) {
	row := strings.Split("17,Sample,101,102,201,202,301,302,401,402,501,502,601,602,QUICK,700,", ",")
	want := battlemodel.CombatSummonDefinition{ID: 17, Name: "Sample", Initial: [6]int32{101, 201, 301, 401, 501, 601}, Maximum: [6]int32{102, 202, 302, 402, 502, 602}, Ability: 1, UseSkillID: 700}
	got, err := ParseSummon(row)
	if err != nil || got != want {
		t.Fatalf("interleaved summon columns: %+v %v", got, err)
	}
	text := strings.Join(row, ",") + "\n"
	for _, invalid := range []string{text + text, "# empty\n", strings.Replace(text, "QUICK", "FUTURE", 1), strings.Replace(text, ",601,", ",2147483648,", 1)} {
		catalog := &battlemodel.Definitions{}
		if err := applySummonTable(catalog, map[string]string{SummonsTable: invalid}); err == nil || catalog.Summons != nil {
			t.Fatal("invalid summon input was partially installed")
		}
	}
	catalog := &battlemodel.Definitions{}
	if err := applySummonTable(catalog, map[string]string{SummonsTable: text}); err != nil || !reflect.DeepEqual(catalog.Summons, map[int]battlemodel.CombatSummonDefinition{17: want}) {
		t.Fatal("complete summon table was not installed")
	}
}

// Optional source gate for the exact table already present in the editable
// Unity master set; ordinary tests never search for local game resources.
func TestCanonicalSummonSource(t *testing.T) {
	path := os.Getenv("CLIENT_REBUILD_SUMMONS_TABLE")
	if path == "" {
		t.Skip("requires the canonical modern summon table")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	catalog := &battlemodel.Definitions{}
	if err := applySummonTable(catalog, map[string]string{SummonsTable: string(raw)}); err != nil {
		t.Fatal(err)
	}
	t.Logf("canonical summons loaded: %d", len(catalog.Summons))
}
