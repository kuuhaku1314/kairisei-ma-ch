package httpapi

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPresentationExtensionKeepsRequiredAndUnknownFieldChecks(t *testing.T) {
	for _, sample := range []struct {
		body string
		ok   bool
		want int8
	}{
		{`{"bossid":1}`, true, 0},
		{`{"bossid":1,"is_battle_msg_skip":1}`, true, 1},
		{`{"is_battle_msg_skip":1}`, false, 0},
		{`{"bossid":1,"unrelated":1}`, false, 0},
		{`{"bossid":1,"is_battle_msg_skip":"yes"}`, false, 0},
	} {
		var value struct {
			BossID int  `json:"bossid"`
			Skip   int8 `json:"is_battle_msg_skip"`
		}
		request := httptest.NewRequest("POST", "/TeamBattleMultiRoomCreate", strings.NewReader(sample.body))
		err := decodeExact(request, []string{"bossid"}, &value, "is_battle_msg_skip")
		if (err == nil) != sample.ok || sample.ok && value.Skip != sample.want {
			t.Fatalf("%s: %+v %v", sample.body, value, err)
		}
	}
}
