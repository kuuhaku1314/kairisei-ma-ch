package battlecontent

import (
	"io"
	"reflect"
	"strings"
	"testing"

	"kairisei.local/server/battlemodel"
)

// The two public content boundaries must describe the same existing card,
// including its skill references and profile tags. Distinct JP-only fields
// make an accidental old-column read fail instead of returning plausible zeros.
func TestModernCardColumnsPreserveExistingBattleDefinition(t *testing.T) {
	legacy := make([]string, 99)
	modern := make([]string, 119)
	for column, value := range map[int]string{0: "42", 1: "41", 7: "MILLION_RARE", 10: "100", 11: "200", 13: "300", 14: "400", 16: "500", 17: "600", 19: "700", 20: "800"} {
		legacy[column], modern[column] = value, value
	}
	for _, field := range []struct {
		old, current int
		value        string
	}{{23, 35, "65"}, {24, 36, "1000"}, {26, 38, "123"}, {27, 39, "456"}, {28, 40, "710"}, {29, 41, "720"}, {30, 42, "730"}, {31, 43, "740"}, {32, 44, "810"}, {33, 45, "820"}, {54, 68, "7"}, {55, 69, "11"}, {61, 75, "19"}} {
		legacy[field.old], modern[field.current] = field.value, field.value
	}
	modern[23], modern[26], modern[27] = "9991", "9992", "9993"
	modern[46], modern[49], modern[113], modern[118] = "910", "920", "SPECIAL", "1"
	modern[8], modern[34] = "GREEN", "1"
	want, err := ParseCard(legacy)
	if err != nil {
		t.Fatal(err)
	}
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
func TestCompleteModernCardMasterIsNotTruncatedAtLegacyLimit(t *testing.T) {
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

func TestCardPresentationLegacyHeaderAndModernLayout(t *testing.T) {
	legacy := make([]string, 99)
	legacy[0], legacy[7], legacy[22], legacy[36] = "42", "MILLIONRARE", "1", "10001234"
	got, err := ParseCardPresentation(legacy)
	if err != nil || got.PictureID != 10001234 || got.LimitCount != 1 || got.Modern {
		t.Fatalf("legacy presentation: %+v %v", got, err)
	}
	header := append([]string(nil), legacy...)
	header[0], header[36] = "#ID", "PictID"
	if err := ValidateCardPresentationHeader(header); err != nil {
		t.Fatal(err)
	}
	header[36] = "OtherColumn"
	if err := ValidateCardPresentationHeader(header); err == nil {
		t.Fatal("accepted shifted named header")
	}
	if err := ValidateCardPresentationHeader(legacy); err != nil {
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
	for _, invalid := range []string{text + text, strings.Join(row, ",") + "\n", "# empty\n", strings.Join(row[:99], ",") + "\n"} {
		candidate := &battlemodel.Definitions{Cards: map[int]battlemodel.CombatCardDefinition{42: ordinary}}
		if err := applyAlchemistCardTable(candidate, map[string]string{"card_alchemist.csv": invalid}); err == nil {
			t.Fatal("accepted a colliding, empty or legacy-layout alchemist table")
		}
	}
}
