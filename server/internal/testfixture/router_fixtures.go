package testfixture

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"kairisei.local/server/internal/gamestate"
)

func WriteTestCardMaster(t *testing.T, root string) string {
	t.Helper()
	cardTemplate := func(cardID int, name string) map[string]any {
		return map[string]any{
			"unique_id": 0, "card_id": cardID, "name": name,
			"same_card_id": cardID, "same_support_card_id": cardID,
			"rarity_rank": 1, "skill_level_max": 1,
			"level": 1, "level_max": 60, "experience_table_id": 1,
			"experience": 0, "now_level_experience": 0, "love": 0,
			"love_max": 10000, "premium_rarity": false,
			"skill_levels": []int{1}, "hp": 103, "attack": 12,
			"magic": 2, "mind": 1, "next_level_experience": 10,
			"parameter_initial":            map[string]any{"hp": 100, "attack": 10, "magic": 0, "mind": 0},
			"parameter_maximum":            map[string]any{"hp": 200, "attack": 20, "magic": 0, "mind": 0},
			"parameter_love_maximum_bonus": map[string]any{"hp": 0, "attack": 0, "magic": 0, "mind": 0},
			"add_experience":               10, "base_add_price": 100, "sell_gold": 500, "fame": 1,
			"fame_max": 100, "development_type": 1, "decompose_radix": 20,
			"develop_radix": 0, "acquisition_text": "Test source",
		}
	}
	stackTemplate := func(cardID int) map[string]any {
		return map[string]any{
			"cardid": cardID, "num": 0, "hp": 5, "atkp": 5,
			"intp": 5, "mndp": 5, "add_exp": 10, "base_add_price": 500,
		}
	}
	buddyStackTemplate := func(cardID int, materialType int, addExperience int) map[string]any {
		return map[string]any{
			"cardid": cardID, "num": 0, "hp": 0, "atkp": 0, "intp": 0, "mndp": 0,
			"add_exp": addExperience, "base_add_price": 100, "material_type": materialType,
		}
	}
	sphereDefinition := func(sphereID int) map[string]any {
		return map[string]any{
			"sphrid": sphereID, "same_sphrid": sphereID, "name": "Test sphere",
			"text": "", "type": "NORMAL", "rarity": "NORMAL", "evo_count": 0,
			"max_level": 1, "exp_table_id": 0, "equip_allowed": []bool{true, true, true, true},
			"skill_id": sphereID, "passive_skill_id": 0, "call_skill_id": 0,
			"pict_id": sphereID, "count": 1, "play_condition": "TURN_AFTER",
			"play_condition_param": 1, "way_to_use_text": "", "sell_gold": 100,
			"evolution_id": 0, "material_add_experience": 10, "fusion_base_add_price": 10,
		}
	}
	sphereSeed := func(uniqueID int64, sphereID int) map[string]any {
		return map[string]any{
			"unique_id": uniqueID, "sphere_id": sphereID, "level": 1, "experience": 0,
			"next_level_experience": 0, "now_level_experience": 0,
			"add_experience": 10, "base_add_price": 10, "is_lock": 0, "create_time": 0,
		}
	}
	buddyDefinition := map[string]any{
		"buddyid": 1000010, "same_buddyid": 1000010, "name": "Test buddy", "rarity": "LEGEND",
		"evolution_count": 0, "max_level": 1, "pict_id": 10000010, "experience_table_id": 401,
		"sell_gold": 100000, "evolution_id": 0, "overlimit_item_id": 6191, "overlimit_item_num": 1,
		"material_add_experience": 100, "fusion_base_add_price": 500,
	}
	buddySeed := map[string]any{
		"unique_id": 1, "buddy_id": 1000010, "level": 1, "experience": 0,
		"next_level_experience": 0, "now_level_experience": 0,
		"add_experience": 100, "base_add_price": 500, "is_lock": 0, "create_time": 0,
	}
	cardTemplates := []map[string]any{
		cardTemplate(10000013, "Test tutorial mercenary"),
		cardTemplate(10000025, "Test tutorial millionaire"),
		cardTemplate(10000033, "Test tutorial thief"),
		cardTemplate(10000037, "Test tutorial singer"),
		cardTemplate(10000010, "Test source"),
		cardTemplate(10000011, "Test target"),
		cardTemplate(10000018, "Test friend gacha two"),
		cardTemplate(10000022, "Test friend gacha three"),
		cardTemplate(10000026, "Test friend gacha four"),
		cardTemplate(10000030, "Test friend gacha five"),
		cardTemplate(10000034, "Test friend gacha six"),
		cardTemplate(10000038, "Test friend gacha seven"),
		cardTemplate(10000050, "Test friend gacha eight"),
		cardTemplate(10000054, "Test friend gacha nine"),
		cardTemplate(10000061, "Test friend gacha ten"),
		cardTemplate(10000123, "Test friend gacha eleven"),
		cardTemplate(10000304, "Test friend gacha twelve"),
		cardTemplate(10105002, "Test gacha one"),
		cardTemplate(10105009, "Test gacha two"),
		cardTemplate(10105012, "Test gacha three"),
		cardTemplate(10105016, "Test gacha four"),
	}
	for index := 1; index <= 49; index++ {
		cardTemplates = append(cardTemplates, cardTemplate(11000000+index, fmt.Sprintf("Test trade card %d", index)))
	}
	seenCardIDs := make(map[int]struct{}, len(cardTemplates))
	for _, template := range cardTemplates {
		seenCardIDs[template["card_id"].(int)] = struct{}{}
	}
	var seed struct {
		Cards []struct {
			CardID int    `json:"card_id"`
			Name   string `json:"name"`
		} `json:"cards"`
		ContainerCards []struct {
			CardID int    `json:"card_id"`
			Name   string `json:"name"`
		} `json:"container_cards"`
		Gachas []struct {
			CardIDs    []int                      `json:"cardids"`
			RewardPool []gamestate.WeightedReward `json:"reward_pool"`
		} `json:"gachas"`
	}
	seedContent, err := os.ReadFile(filepath.Join("..", "..", "config", "cn602-save-template.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(seedContent, &seed); err != nil {
		t.Fatal(err)
	}
	appendSeedTemplate := func(cardID int, name string) {
		if _, exists := seenCardIDs[cardID]; exists {
			return
		}
		seenCardIDs[cardID] = struct{}{}
		cardTemplates = append(cardTemplates, cardTemplate(cardID, name))
	}
	for _, card := range seed.Cards {
		appendSeedTemplate(card.CardID, card.Name)
	}
	for _, card := range seed.ContainerCards {
		appendSeedTemplate(card.CardID, card.Name)
	}
	for _, gacha := range seed.Gachas {
		for _, cardID := range gacha.CardIDs {
			appendSeedTemplate(cardID, fmt.Sprintf("Test gacha card %d", cardID))
		}
		for _, entry := range gacha.RewardPool {
			if entry.Reward.Type == 6 {
				appendSeedTemplate(entry.Reward.RewardTypeID, "Test mixed gacha card")
			}
		}
	}
	master := map[string]any{
		"card_collection_pages": [][]int{{10000010, 0, 0, 0, 0, 0, 0, 0, 0, 0}},
		"card_progression_policy": map[string]any{
			"fusion_gold_per_material_per_base_level": 100,
			"maximum_card_material_count":             100,
			"fusion_success_types": []map[string]any{
				{"success_type": 0, "weight": 90, "experience_permille": 1000},
				{"success_type": 1, "weight": 9, "experience_permille": 1500},
				{"success_type": 2, "weight": 1, "experience_permille": 2500},
			},
			"fame_normal":  map[string]any{"hp": 300, "attack": 200, "magic": 200, "mind": 100},
			"fame_premium": map[string]any{"hp": 600, "attack": 400, "magic": 400, "mind": 200},
		},
		"card_experience_tables": map[string]any{"1": func() []int {
			values := make([]int, 59)
			for index := range values {
				values[index] = 10
			}
			return values
		}()},
		"sphere_progression_policy": map[string]any{
			"material_level_bonus_permille_per_level": 100,
		},
		"buddy_progression_policy": map[string]any{
			"maximum_material_count": 100,
		},
		"support_deck_set_card_num": 3,

		"card_templates": cardTemplates,
		"card_category_profiles": []map[string]any{{
			"categoryid": 60200001, "name": "Test card series", "order": 2,
			"view_type": 0,
		}},
		"card_group_profiles": []map[string]any{
			{"groupid": 20020001, "categoryid": 60200001, "name": "Test 1", "order": 10, "deck_limit_bossid": 0, "member_cardids": []int{10000010}},
			{"groupid": 30020001, "categoryid": 60200001, "name": "Test 2", "order": 11, "deck_limit_bossid": 0, "member_cardids": []int{10000010}},
			{"groupid": 30020002, "categoryid": 60200001, "name": "Test 3", "order": 12, "deck_limit_bossid": 0, "member_cardids": []int{10000010}},
			{"groupid": 30020003, "categoryid": 60200001, "name": "Test 4", "order": 13, "deck_limit_bossid": 0, "member_cardids": []int{10000010}},
			{"groupid": 30020005, "categoryid": 60200001, "name": "Test 5", "order": 14, "deck_limit_bossid": 0, "member_cardids": []int{10000010}},
			{"groupid": 90020001, "categoryid": 60200001, "name": "Test 6", "order": 15, "deck_limit_bossid": 0, "member_cardids": []int{10000010}},
		},

		"stack_card_templates": []map[string]any{
			stackTemplate(20000006),
			stackTemplate(20000011),
			stackTemplate(20000016),
			buddyStackTemplate(20005001, 10, 10),
			buddyStackTemplate(20005002, 9, 7200),
		},
		"evolution_transitions": []map[string]any{{
			"from_cardid":    10000010,
			"to_cardid":      10000011,
			"evolution_type": 0,
			"gold":           100,
			"materials": []map[string]any{{
				"cardid": 20000006,
				"num":    1,
			}},
		}},
		"card_development_policy": map[string]any{
			"time_every_fame_seconds": 3600,
			"coin_every_hour":         1,
			"help_path":               "/disabled/web",
		},
		"skipped_incomplete_evolution_source_cardids": []int{},
		"sphere_definitions": []map[string]any{
			sphereDefinition(15000010), sphereDefinition(15000020), sphereDefinition(15000030),
		},
		"sphere_experience_tables": map[string]any{"1": []int{1}},
		"sphere_evolution_prices": map[string]any{
			"NORMAL": []int{1, 2, 3, 4}, "RARE": []int{1, 2, 3, 4}, "MILLIONRARE": []int{1, 2, 3, 4},
		},
		"sphere_seed_templates": []map[string]any{
			sphereSeed(603000001, 15000010), sphereSeed(603000002, 15000020), sphereSeed(603000003, 15000030),
		},
		"sphere_seed_decks": map[string]any{
			"1": []int64{603000001, 603000002, 603000003},
			"2": []int64{603000001, 603000002, 603000003},
			"3": []int64{603000001, 603000002, 603000003},
			"4": []int64{603000001, 603000002, 603000003},
		},
		"buddy_definitions":       []map[string]any{buddyDefinition},
		"buddy_experience_tables": map[string]any{"401": []int{1}},
		"buddy_evolution_prices": map[string]any{
			"NORMAL": []int{1, 2, 3, 4, 5}, "RARE": []int{1, 2, 3, 4, 5},
			"MILLIONRARE": []int{1, 2, 3, 4, 5}, "EXRARE": []int{1, 2, 3, 4, 5},
			"LEGEND": []int{1, 2, 3, 4, 5},
		},
		"buddy_seed_templates": []map[string]any{buddySeed},
		"buddy_seed_decks": map[string]any{
			"1": []int64{1, 0, 0, 0, 0}, "2": []int64{1, 0, 0, 0, 0},
			"3": []int64{1, 0, 0, 0, 0}, "4": []int64{1, 0, 0, 0, 0},
		},
		"buddy_seed_stack_cards": []map[string]any{
			{"cardid": 20005001, "num": 1, "hp": 0, "atkp": 0, "intp": 0, "mndp": 0, "add_exp": 10, "base_add_price": 100, "material_type": 10},
			{"cardid": 20005002, "num": 100, "hp": 0, "atkp": 0, "intp": 0, "mndp": 0, "add_exp": 7200, "base_add_price": 100, "material_type": 9},
		},
		"support_deck_slot_unlock_rules": []map[string]any{
			{"slot_index": 1, "level": 70, "card_collection_num": 300, "gold": 500000},
			{"slot_index": 2, "level": 100, "card_collection_num": 400, "gold": 1000000},
			{"slot_index": 3, "level": 130, "card_collection_num": 500, "gold": 1000000},
		},
	}
	// Bootstrap fixtures resolve the newly configured reward identities using
	// their existing minimal definitions; production closure is checked separately.
	for _, spec := range []struct {
		key, idKey string
		kind       int
	}{
		{"buddy_definitions", "buddyid", 19}, {"sphere_definitions", "sphrid", 15}, {"stack_card_templates", "cardid", 13},
	} {
		rows := master[spec.key].([]map[string]any)
		seen := map[int]bool{}
		for _, row := range rows {
			seen[row[spec.idKey].(int)] = true
		}
		for _, gacha := range seed.Gachas {
			for _, entry := range gacha.RewardPool {
				id := entry.Reward.RewardTypeID
				if entry.Reward.Type != spec.kind || seen[id] {
					continue
				}
				seen[id] = true
				copy := map[string]any{}
				for k, v := range rows[0] {
					copy[k] = v
				}
				copy[spec.idKey] = id
				if spec.kind == 19 {
					copy["same_buddyid"] = id
				}
				if spec.kind == 15 {
					copy["same_sphrid"] = id
				}
				rows = append(rows, copy)
			}
		}
		master[spec.key] = rows
	}
	content, err := json.Marshal(master)
	if err != nil {
		t.Fatal(err)
	}
	cardMasterPath := filepath.Join(root, "cn602-card-runtime-master.json")
	if err := os.WriteFile(cardMasterPath, content, 0o600); err != nil {
		t.Fatal(err)
	}
	return cardMasterPath
}

func WriteTestExploreMaster(t *testing.T, root string) string {
	t.Helper()
	reward := map[string]any{
		"type": 4, "num": 10, "reward_typeid": 0,
		"card_lv": 0, "card_fame": 0, "card_love": 0,
		"card_skill_lv": []int{},
	}
	master := map[string]any{
		"maps": []map[string]any{{
			"id": 1000, "map_type": "GRASS", "map_ids": []int{1000, 1000}, "bgm_id": 10020,
		}},
		"stages": []map[string]any{{
			"explore_stageid": 1000, "stage_name": "Test Explore",
			"stage_mapid": 1000, "stage_type": 0, "state_flag": 0,
			"limit_sec": 60,
			"floors":    []map[string]any{{"explore_floorid": 1000, "floor_name": "Test Floor"}},
		}},
		"floor_rarity": 0,
		"events": []map[string]any{{
			"treasureboxes": []map[string]any{{
				"reward": []map[string]any{reward}, "bonus": []any{}, "talk_cardid": 10000010, "is_rare": 0,
			}},
			"symbols": []any{},
		}},
	}
	content, err := json.Marshal(master)
	if err != nil {
		t.Fatal(err)
	}
	masterPath := filepath.Join(root, "cn602-explore-runtime-master.json")
	if err := os.WriteFile(masterPath, content, 0o600); err != nil {
		t.Fatal(err)
	}
	return masterPath
}

func WriteTestStoryMaster(t *testing.T, root string) string {
	t.Helper()
	story := func(id int) map[string]any {
		return map[string]any{
			"story_mainid": id, "state_flag": 0, "title": "第1话",
			"unlock_text": "", "reward_type_flag": 0,
		}
	}
	part := func(id int, name string, storyID int) []map[string]any {
		return []map[string]any{{
			"story_main_partid": id,
			"name":              name,
			"sections": []map[string]any{{
				"story_main_sectionid": id * 100,
				"section_title":        "序章",
				"stories":              []map[string]any{story(storyID)},
			}},
		}}
	}
	master := map[string]any{
		"normal_parts": part(1, "第一部", 10001),
		"cn_parts":     part(1, "国服专属剧情", 100001),
		"sub_characters": []map[string]any{{
			"story_main_charaid": 1, "name": "角色剧情", "pictid": 0,
			"sections": []map[string]any{{
				"story_sub_sectionid": 10000010, "story_sub_section_type": 0,
				"section_title": "Test Card", "pictid": 10000010,
				"stories": []map[string]any{{
					"story_subid": 20010110, "state_flag": 0, "title": "Test Sub Story",
					"unlock_text": "获得对应卡牌后解锁",
					"feature_reward": map[string]any{
						"type": 10, "num": 1, "reward_typeid": 0, "card_lv": 0,
						"card_fame": 0, "card_love": 0, "card_skill_lv": []int{},
					},
				}},
			}},
		}},

		"events": []map[string]any{{
			"story_eventid": 100964, "name": "Test Event", "pictid": 10000010,
			"stories": []map[string]any{{
				"sub": map[string]any{
					"story_subid": 10096410, "state_flag": 8, "title": "Test Event Story",
					"unlock_text": "",
					"feature_reward": map[string]any{
						"type": 10, "num": 1, "reward_typeid": 0, "card_lv": 0,
						"card_fame": 0, "card_love": 0, "card_skill_lv": []int{},
					},
				},
				"materials": []any{},
			}},
		}},

		"reward_policy": map[string]any{

			"main_first_clear": map[string]any{
				"type": 10, "num": 1, "reward_typeid": 0, "card_lv": 0,
				"card_fame": 0, "card_love": 0, "card_skill_lv": []int{},
			},
			"sub_first_clear": map[string]any{
				"type": 10, "num": 1, "reward_typeid": 0, "card_lv": 0,
				"card_fame": 0, "card_love": 0, "card_skill_lv": []int{},
			},
			"event_first_clear": map[string]any{
				"type": 10, "num": 1, "reward_typeid": 0, "card_lv": 0,
				"card_fame": 0, "card_love": 0, "card_skill_lv": []int{},
			},
		},
		"story_battles": map[string]any{
			"official_ids": []int{60004110},

			"references": []map[string]any{{
				"story_kind": "sub", "story_id": 10096410,
				"story_battle_ids": []int{60004110},
			}},
		},
		"event_page_profile": map[string]any{
			"eventid": 0, "bg_pictid": 0, "home_button_pictid": 0,
			"end_time": 2147483647, "info_url": "",
			"update_info": "本地活动中心", "itemid": []int{},
			"buttons": []any{}, "is_new_solo": 0, "is_new_multi": 0,
		},
	}
	content, err := json.Marshal(master)
	if err != nil {
		t.Fatal(err)
	}
	masterPath := filepath.Join(root, "cn602-story-runtime-master.json")
	if err := os.WriteFile(masterPath, content, 0o600); err != nil {
		t.Fatal(err)
	}
	return masterPath
}

func WriteTestNaviMaster(t *testing.T, root string) string {
	t.Helper()
	navigators := make([]map[string]any, 0, 49)
	for naviID := 0; naviID < 49; naviID++ {
		voiceID := naviID
		if naviID == 41 {
			voiceID = 18
		}
		folder := fmt.Sprintf("navi_%d", naviID)
		navigators = append(navigators, map[string]any{
			"navi_id":       naviID,
			"name":          fmt.Sprintf("Test Navi %d", naviID),
			"pict_id":       50000000 + naviID*1000,
			"item_pict_id":  30000 + naviID,
			"live2d_folder": folder,
			"live2d_bundle": "live2d/live2d_" + folder + ".dat",

			"voice_id":         voiceID,
			"client_published": true,
		})
	}
	master := map[string]any{
		"local_purchase": map[string]any{

			"currency": "CRYSTAL",
			"price":    50,
		},

		"navigators": navigators,
	}
	content, err := json.Marshal(master)
	if err != nil {
		t.Fatal(err)
	}
	masterPath := filepath.Join(root, "cn602-navi-runtime-master.json")
	if err := os.WriteFile(masterPath, content, 0o600); err != nil {
		t.Fatal(err)
	}
	return masterPath
}

func WriteTestItemMaster(t *testing.T, root string) string {
	t.Helper()
	item := func(itemID int, name string, pictID int, itemType string) map[string]any {
		return map[string]any{
			"item_id": itemID, "name": name, "pict_id": pictID,
			"item_type": itemType, "function": "", "function_value": 0,
			"love_up_price": 0,
			"max_owned":     9999, "daily_limited": 0, "description": name,
		}
	}
	userBuffProfiles := make([]map[string]any, 527)
	for index := range userBuffProfiles {
		userBuffProfiles[index] = map[string]any{
			"user_buff_id": index + 1, "item_id": 1000, "required_num": 1,
			"not_enough_error_text": "test", "duration_seconds": 1800,
		}
	}
	tradeShopLineups := make([]map[string]any, 49)
	for index := range tradeShopLineups {
		tradeShopLineups[index] = map[string]any{
			"lineupid": 60290002 + index, "lineup_name": fmt.Sprintf("Test trade card %d", index+1),
			"stock_num": 1, "is_lineup_new": 0, "is_lineup_old": 0, "pictid": 0,
			"prices": []map[string]any{{"type": 4, "id": 4000, "num": 1000, "point_card_condition": []map[string]any{}}},
			"rewards": []map[string]any{{
				"type": 6, "num": 1, "reward_typeid": 11000001 + index,
				"card_lv": 1, "card_fame": 1, "card_love": 0, "card_skill_lv": []int{1},
			}},
		}
	}
	exchangeItemIDs := []int{
		6046, 6049, 6060, 6099,
		8829, 8830, 8831, 8832, 8833, 8834, 8835,
		8919, 8920, 8921, 8922, 8923, 8991,
	}
	exchangeProfiles := make([]map[string]any, len(exchangeItemIDs))
	crystalGachaItem := item(9010, "水晶扭蛋币", 90010, "GACHA_TICKET")
	crystalGachaItem["description"] = "可在水晶扭蛋币商店中兑换获得"
	testItems := []map[string]any{
		item(1000, "Test BP Heal", 10080, "BP_HEAL"),
		item(1200, "Test AP Heal", 10010, "AP_HEAL"),
		item(2000, "Test Gacha Ticket", 10060, "GACHA_TICKET"),
		item(2001, "Test First Gacha Ticket", 10060, "GACHA_TICKET"),
		item(2002, "Test Attribute Gacha Ticket", 10062, "GACHA_TICKET"),
		item(3999, "骑士徽章", 19001, "EVENT_POINT"),
		item(4000, "大硬币", 20010, "EVENT_POINT"),
		item(6201, "Test Medal", 16201, "GACHA_TICKET"),
		item(7025, "Test Fragment", 17025, "EVENT_POINT"),
		item(6202, "Test Rare Gacha Ticket", 16202, "GACHA_TICKET"),
		item(6203, "Test Unowned Gacha Ticket", 16203, "GACHA_TICKET"),
		crystalGachaItem,
	}
	for index, itemID := range exchangeItemIDs {
		testItems = append(testItems, item(itemID, fmt.Sprintf("Test exchange %d", itemID), 60000+index, "EVENT_POINT"))
		exchangeProfiles[index] = map[string]any{
			"item_id": itemID, "is_appear_event": 0, "eventid": 60280000 + itemID,
			"need_num": 1,
			"reward": map[string]any{
				"type": 6, "num": 1, "reward_typeid": 11000001 + index,
				"card_lv": 1, "card_fame": 1, "card_love": 0, "card_skill_lv": []int{1},
			},
		}
	}
	master := map[string]any{
		"boss_coin_item_id": 4000,

		"user_buff_profiles":  userBuffProfiles,
		"items":               testItems,
		"item_gacha_profiles": []map[string]any{},

		"item_exchange_profiles": exchangeProfiles,

		"item_lack_tip_profiles": []map[string]any{
			{"idx": 1, "title": "徽章不足", "desc": "test", "way": "test", "txt_url_info": []map[string]any{{"txt": "暂不前往", "url": ""}}},
			{"idx": 2, "title": "水晶不足", "desc": "test", "way": "test", "txt_url_info": []map[string]any{{"txt": "暂不前往", "url": ""}}},
			{"idx": 3, "title": "金币不足", "desc": "test", "way": "test", "txt_url_info": []map[string]any{{"txt": "暂不前往", "url": ""}}},
			{"idx": 4, "title": "强化素材不足", "desc": "test", "way": "test", "txt_url_info": []map[string]any{{"txt": "暂不前往", "url": ""}}},
			{"idx": 5, "title": "体力不足", "desc": "test", "way": "test", "txt_url_info": []map[string]any{{"txt": "暂不前往", "url": ""}}},
			{"idx": 6, "title": "钥匙不足", "desc": "test", "way": "test", "txt_url_info": []map[string]any{{"txt": "暂不前往", "url": ""}}},
			{"idx": 7, "title": "扭蛋券不足", "desc": "test", "way": "test", "txt_url_info": []map[string]any{{"txt": "暂不前往", "url": ""}}},
			{"idx": 8, "title": "水晶扭蛋币不足", "desc": "test", "way": "test", "txt_url_info": []map[string]any{{"txt": "暂不前往", "url": ""}}},
		},

		"event_shop_profiles": []map[string]any{},

		"trade_shop_profiles": []map[string]any{{
			"trade_shopid": 60290001, "name": "BOSS币交换所", "text": "Test trade shop",
			"shop_type": 1, "tab_type": 0, "end_time": 2147483647, "is_new": 0, "pictid": 0,
			"lineups": tradeShopLineups,
		}},
	}
	content, err := json.Marshal(master)
	if err != nil {
		t.Fatal(err)
	}
	masterPath := filepath.Join(root, "cn602-item-runtime-master.json")
	if err := os.WriteFile(masterPath, content, 0o600); err != nil {
		t.Fatal(err)
	}
	return masterPath
}

func WriteTestAvatarMaster(t *testing.T, root string) string {
	t.Helper()
	part := func(partID int, arthurMask int, slotMask int) map[string]any {
		return map[string]any{
			"part_id": partID, "name": fmt.Sprintf("Test Avatar part %d", partID),
			"arthur_mask": arthurMask, "slot_mask": slotMask, "icon_pict_id": partID,
			"is_new_list": 0, "series_id": 0,
		}
	}
	master := map[string]any{
		"initial_owned_part_ids": []int{1, 2, 3, 4, 11, 12, 13, 14},
		"default_decks": map[string]any{
			"1": []int{0, 1, 1, 0, 0, 0, 0}, "2": []int{11, 2, 2, 0, 11, 0, 0},
			"3": []int{0, 3, 3, 3, 0, 0, 14}, "4": []int{12, 4, 4, 0, 12, 0, 13},
		},
		"shop_policy": map[string]any{
			"pay_type": 1, "pay_typeid": 0, "price": 1000,
			"appear_end": 0, "new_appear_end": 0,
		},
		"part_definitions": []map[string]any{
			part(1, 2, 6), part(2, 4, 6), part(3, 8, 14), part(4, 16, 6),
			part(11, 4, 17), part(12, 16, 17), part(13, 16, 64), part(14, 8, 64),
		},
		"series": []map[string]any{{"series_id": 0, "name": "Test"}},
		"costume_rewards": []gamestate.CollectionRewardDefinition{
			{Type: 14, ID: 1, Name: "Test Costume 1", PictID: 40001},
			{Type: 14, ID: 2, Name: "Test Costume 2", PictID: 40002},
		},
		"series_completions": []map[string]any{{
			"completion_id": 1, "name": "Test completion", "part_ids": []int{1},
			"rewards": []map[string]any{{"type": "SPHR", "num": 1, "reward_id": 15000010}},
		}},
	}
	content, err := json.Marshal(master)
	if err != nil {
		t.Fatal(err)
	}
	masterPath := filepath.Join(root, "cn602-avatar-runtime-master.json")
	if err := os.WriteFile(masterPath, content, 0o600); err != nil {
		t.Fatal(err)
	}
	return masterPath
}

func WriteTestStampMaster(t *testing.T, root string) string {
	t.Helper()
	master := map[string]any{
		"category_order": []int{1, 7, 2, 3, 4, 5, 6},
		"stamps": []map[string]any{{
			"stamp_id": 100001, "category": 1, "label": "Test Stamp",
			"display_text": "Test Stamp", "default_owned": true,
		}},
	}
	content, err := json.Marshal(master)
	if err != nil {
		t.Fatal(err)
	}
	masterPath := filepath.Join(root, "cn602-stamp-runtime-master.json")
	if err := os.WriteFile(masterPath, content, 0o600); err != nil {
		t.Fatal(err)
	}
	return masterPath
}

func WriteTestHonorMaster(t *testing.T, root string) string {
	t.Helper()
	master := map[string]any{
		"honors": []map[string]any{{
			"honor_id": 10100001, "name": "Test Honor", "slot_mask": 5,
			"same_card_id": 0, "get_type": 0, "default_owned": true,
		}},
	}
	content, err := json.Marshal(master)
	if err != nil {
		t.Fatal(err)
	}
	masterPath := filepath.Join(root, "cn602-honor-runtime-master.json")
	if err := os.WriteFile(masterPath, content, 0o600); err != nil {
		t.Fatal(err)
	}
	return masterPath
}

func WriteTestSave(t *testing.T, root string) string {
	t.Helper()
	templatePath := filepath.Join("..", "..", "config", "cn602-save-template.json")
	content, err := os.ReadFile(templatePath)
	if err != nil {
		t.Fatal(err)
	}
	savePath := filepath.Join(root, "cn602-save.json")
	if err := os.WriteFile(savePath, content, 0o600); err != nil {
		t.Fatal(err)
	}
	return savePath
}
