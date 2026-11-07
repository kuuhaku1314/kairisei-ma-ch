package httpapi

import (
	"testing"

	"kairisei.local/server/internal/game"
)

func TestSoloResultPartnersExposeCurrentFriendState(t *testing.T) {
	selected := []game.TeamBattleResultPartner{
		{UserID: 1001, Name: "未关注", ArthurType: 1, Level: 1, DeckRank: 1, LeaderCardID: 10, LeaderLevel: 1, LeaderFame: 1},
		{UserID: 1002, Name: "已关注", ArthurType: 2, Level: 1, DeckRank: 1, LeaderCardID: 20, LeaderLevel: 1, LeaderFame: 1},
		{UserID: 1003, Name: "好友", ArthurType: 3, Level: 1, DeckRank: 1, LeaderCardID: 30, LeaderLevel: 1, LeaderFame: 1},
	}
	partners, err := teamBattleSoloResultPartners(selected, map[int]int8{
		1002: game.FriendStateFollow,
		1003: game.FriendStateFriend,
	})
	if err != nil {
		t.Fatal(err)
	}
	wantStates := []int8{game.FriendStateOther, game.FriendStateFollow, game.FriendStateFriend}
	for index, partnerValue := range partners {
		wrapper := battleResultPartnerWire(partnerValue.MemberType, partnerValue.IsOnline, partnerValue.Member)
		if wrapper["member_type"] != index+2 || wrapper["is_online"] != 0 {
			t.Fatalf("partner %d lost its battle position or NPC flag: %+v", index, wrapper)
		}
		partner := wrapper["member"].(map[string]any)
		if got := partner["state"].(int8); got != wantStates[index] {
			t.Fatalf("partner %d state = %d, want %d", index, got, wantStates[index])
		}
	}
}
