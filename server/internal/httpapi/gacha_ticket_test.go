package httpapi

import (
	"strings"
	"testing"

	"kairisei.local/server/internal/game"
	"kairisei.local/server/internal/gamestate"
)

func TestRarityFiveTicketGachaUsesDedicatedLoopbackBanner(t *testing.T) {
	api := &API{baseURL: "http://10.0.2.2:26020"}
	limited := api.gachaInfos([]gamestate.GachaProfile{
		{GachaID: 10, CardNum: 10, CardNumMax: 10, PlayCount: 2, GroupPlayCount: 4, PlayCountMax: 5},
		{GachaID: 11, CardNum: 1, CardNumMax: 10, PlayCount: 2, GroupPlayCount: 4, PlayCountMax: 5},
	})
	for i, wantMax := range []int{5, 0} {
		row := limited[i].(map[string]any)
		if row["play_count"] != 4 || row["play_count_max"] != wantMax {
			t.Fatalf("request quota wire contract: %+v", row)
		}
	}
	infos := api.gachaInfos([]gamestate.GachaProfile{
		{GachaID: 1},
		{GachaID: 2, BannerKey: "five_star_ticket"},
		{GachaID: 3, BannerKey: "element_fire"},
	})
	defaultBanner := infos[0].(map[string]any)["image_l_url"].(string)
	fiveStarBanner := infos[1].(map[string]any)["image_l_url"].(string)
	elementBanner := infos[2].(map[string]any)["image_l_url"].(string)
	if !strings.HasSuffix(defaultBanner, "/local/gacha/banner.png") {
		t.Fatalf("default banner = %q", defaultBanner)
	}
	if !strings.HasSuffix(fiveStarBanner, "/local/gacha/five-star-banner.png") ||
		fiveStarBanner == defaultBanner {
		t.Fatalf("five-star banner = %q, default = %q", fiveStarBanner, defaultBanner)
	}
	if !strings.HasSuffix(elementBanner, "/local/gacha/element_fire.png") ||
		elementBanner == defaultBanner {
		t.Fatalf("element banner = %q, default = %q", elementBanner, defaultBanner)
	}
	api.account = &game.Account{}
	api.gachaBannerVersions = map[string]string{"element_fire": "new-banner-content"}
	versioned := api.gachaInfos([]gamestate.GachaProfile{{GachaID: 3, BannerKey: "element_fire"}})[0].(map[string]any)
	if versioned["image_l_url"] != elementBanner+"?v=new-banner-content" || versioned["image_s_url"] != versioned["image_l_url"] {
		t.Fatalf("versioned banner = %+v", versioned)
	}
	covered := []gamestate.GachaProfile{{GachaID: 4, BannerKey: "element_fire", CoverPath: "gacha-covers/a.png"}}
	for source, want := range map[string]string{"server": "http://10.0.2.2:26020/local/gacha-covers/a.png", "storage": "https://cdn.example.com/k/gacha-covers/a.png"} {
		api.account.ApplyRuntimeSettings(game.RuntimeSettings{GachaCoverSource: source, GachaCoverBaseURL: "https://cdn.example.com/k/"})
		if got := api.gachaInfos(covered)[0].(map[string]any)["image_s_url"]; got != want {
			t.Fatalf("%s cover = %q", source, got)
		}
	}
}

func TestHiddenAndDisabledItemShopOffersAreOmitted(t *testing.T) {
	tabs := []gamestate.ItemShopTab{{Lineup: []gamestate.ItemShopLineup{{Hidden: true}, {Disabled: true}, {LineupID: 3, Price: 12000}}}}
	lineups := itemShopTabsWire(tabs, nil)[0].(map[string]any)["lineup"].([]any)
	if len(lineups) != 1 || lineups[0].(map[string]any)["price"] != 12000 {
		t.Fatalf("visible offers: %+v", lineups)
	}
}
