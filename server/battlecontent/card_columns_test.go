package battlecontent

import (
	"io"
	"reflect"
	"strings"
	"testing"

	"kairisei.local/server/battlemodel"
)

func TestUnifiedCardColumns(t *testing.T) {
	modern := make([]string, 119)
	for column, value := range map[int]string{0: "42", 1: "41", 7: "MILLION_RARE", 10: "100", 11: "200", 13: "300", 14: "400", 16: "500", 17: "600", 19: "700", 20: "800"} {
		modern[column] = value
	}
	for _, field := range []struct {
		current int
		value   string
	}{{35, "65"}, {36, "1000"}, {38, "123"}, {39, "456"}, {40, "710"}, {41, "720"}, {42, "730"}, {43, "740"}, {44, "810"}, {45, "820"}, {68, "7"}, {69, "11"}, {75, "19"}} {
		modern[field.current] = field.value
	}
	modern[23], modern[26], modern[27] = "9991", "9992", "9993"
	modern[46], modern[49], modern[113], modern[118] = "910", "920", "SPECIAL", "1"
	modern[8], modern[34] = "GREEN", "1"
	want := battlemodel.CombatCardDefinition{
		ID: 42, BaseCardID: 41, Rarity: "MILLION_RARE", MaxLevel: 65, LoveMax: 1000,
		NormalSkillID: 123, ArthurSkillID: 456, SupportSkillIDs: [4]int{710, 720, 730, 740},
		CallSkillID: 810, PassiveSkillID: 820,
		HPBase: 100, HPGrowth: 200, AttackBase: 300, AttackGrowth: 400,
		MagicBase: 500, MagicGrowth: 600, RecoveryBase: 700, RecoveryGrowth: 800,
	}
	want.ProfileTags[0], want.ProfileTags[1], want.ProfileTags[7] = 7, 11, 19
	want.ArthurPassiveSkillID, want.SummonID = 910, 920
	want.RarityPlus = 4
	want.HasParameterFamilies = true
	want.AlchemistParameters[1][0] = 9991
	want.AlchemistParameters[1][1], want.AlchemistParameters[2][1] = 9992, 9993
	for _, row := range [][]string{modern, append(append([]string(nil), modern...), ""), modern[:113], append(append([]string(nil), modern[:113]...), "")} {
		got, err := ParseCard(row)
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Fatalf("card columns %d: got=%+v err=%v want=%+v", len(row), got, err, want)
		}
	}
	for _, row := range [][]string{modern[:118], append(append([]string(nil), modern[:113]...), "unknown"), append(append([]string(nil), modern...), "unknown")} {
		if _, err := ParseCard(row); err == nil {
			t.Fatalf("accepted unknown modern row shape: %d", len(row))
		}
	}
}

// A limit reader's EOF must never silently omit the end of a valid modern
// master. Both ends of a table larger than the old CN limit must be consumed.
func TestCompleteCardMasterSizeBoundary(t *testing.T) {
	line := "#" + strings.Repeat(" ", 4095) + "\n"
	padding := strings.Repeat(line, maxCombatMasterBytes/len(line)+1)
	rows := 0
	reader := io.MultiReader(strings.NewReader("42\n"), strings.NewReader(padding), strings.NewReader("43\n"))
	if err := readCombatCardData(reader, func([]string) error { rows++; return nil }); err != nil || rows != 2 {
		t.Fatalf("complete modern card master: rows=%d err=%v", rows, err)
	}
	reader = io.MultiReader(strings.NewReader("42\n"), strings.NewReader(padding), strings.NewReader(padding), strings.NewReader("43\n"))
	if err := readCombatCardData(reader, func([]string) error { return nil }); err == nil {
		t.Fatal("accepted an oversized card master after a limit-reader EOF")
	}
}

func TestUnifiedCardPresentation(t *testing.T) {
	row := make([]string, 119)
	row[0], row[7], row[34], row[50] = "42", "MILLIONRARE", "1", "10001234"
	got, err := ParseCardPresentation(row)
	if err != nil || got.PictureID != 10001234 || got.LimitCount != 1 {
		t.Fatalf("card presentation: %+v %v", got, err)
	}
	header := append([]string(nil), row...)
	header[0], header[50] = "#ID", "PictID"
	if err := ValidateCardPresentationHeader(header); err != nil {
		t.Fatal(err)
	}
	header[50] = "OtherColumn"
	if err := ValidateCardPresentationHeader(header); err == nil {
		t.Fatal("accepted shifted named header")
	}
	if err := ValidateCardPresentationHeader(row); err != nil {
		t.Fatal(err)
	}
}

func TestAlchemistCardsShareOrdinaryIdentityWithoutReplacement(t *testing.T) {
	row := make([]string, 113)
	row[0], row[7], row[22], row[23], row[46], row[49] = "123", "MILLIONRARE", "100", "200", "910", "17"
	text := strings.Join(row, ",") + "\n"
	ordinary := battlemodel.CombatCardDefinition{ID: 42, HPBase: 300}
	catalog := &battlemodel.Definitions{Cards: map[int]battlemodel.CombatCardDefinition{42: ordinary}}
	if err := applyAlchemistCardTable(catalog, map[string]string{"card_alchemist.csv": text}); err != nil {
		t.Fatal(err)
	}
	if catalog.Cards[42] != ordinary || catalog.Cards[123].AlchemistParameters[0][0] != 100 ||
		catalog.Cards[123].AlchemistParameters[1][0] != 200 || catalog.Cards[123].SummonID != 17 ||
		catalog.Cards[123].ArthurPassiveSkillID != 910 {
		t.Fatal("alchemist loading changed an ordinary identity or lost modern fields")
	}
	row[0] = "42"
	for _, invalid := range []string{text + text, strings.Join(row, ",") + "\n", "# empty\n"} {
		candidate := &battlemodel.Definitions{Cards: map[int]battlemodel.CombatCardDefinition{42: ordinary}}
		if err := applyAlchemistCardTable(candidate, map[string]string{"card_alchemist.csv": invalid}); err == nil {
			t.Fatal("accepted a colliding or empty alchemist table")
		}
	}
}
