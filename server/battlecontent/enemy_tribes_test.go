package battlecontent

import (
	"reflect"
	"testing"
)

func TestModernEnemyColumnsPreserveGaugeIdentity(t *testing.T) {
	row := make([]string, 33)
	row[0], row[1], row[2], row[3], row[4] = "42", "105", "1", "2", "ems_03901"
	row[8], row[9], row[10], row[22], row[23], row[24] = "enemy", "FIRE", "4500000000", "10000001", "TRANCE", "100"
	row[25], row[26], row[27], row[28], row[29] = "10000", "5000", "3", "20", "L"
	value, err := ParseEnemy(row)
	if err != nil || !reflect.DeepEqual(value.TribeIDs, []int{105, 1, 2}) || value.ModelID != "ems_03901" ||
		value.HP != 4500000000 || value.GaugeReactionID != 10000001 || value.InitialGaugeState != 2 || value.InitialGaugeRate != 100 ||
		value.TranceLimit != 10000 || value.OverheatLimit != 5000 || value.OverheatTurns != 3 || value.OverheatResist != 20 || value.Size != "L" {
		t.Fatal(value, err)
	}
	row[2], row[3] = "", ""
	value, err = ParseEnemy(row[:32])
	if err != nil || !reflect.DeepEqual(value.TribeIDs, []int{105, 0, 0}) {
		t.Fatal(value, err)
	}
	row[23] = "UNKNOWN"
	if _, err := ParseEnemy(row); err == nil {
		t.Fatal("unknown modern gauge state accepted")
	}
}
