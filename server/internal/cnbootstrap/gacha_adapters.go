package cnbootstrap

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"

	"kairisei.local/server/internal/httpapi"
	operationservice "kairisei.local/server/internal/operations"
)

func cnGachaPublicationBusiness(business http.Handler, operations *operationservice.Runtime) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		active, err := operations.GachaPublication()
		if err != nil {
			http.Error(writer, "read local gacha publication", http.StatusInternalServerError)
			return
		}
		business.ServeHTTP(writer, httpapi.WithGachaPublication(request, func(id int) bool {
			return operations.GachaIDPublished(id, active)
		}))
	})
}

var cnGachaCategoryFieldMap = map[string]string{
	"0": "category_num", "1": "pictid",
}

var cnGachaInfoFieldMap = map[string]string{
	"0": "gachaid", "1": "gacha_name", "2": "buymsg", "3": "submsg",
	"4": "category_num", "5": "order_num", "6": "groupid", "7": "gacha_type",
	"8": "arthur_type", "9": "pay_type", "10": "pay_typeid", "11": "price",
	"12": "card_num", "13": "card_num_max", "14": "play_count",
	"15": "play_count_priority_price", "16": "play_count_1day", "17": "play_count_max",
	"18": "play_count_1day_max", "19": "user_select_max", "20": "is_stepup_price",
	"21": "is_daily_first", "22": "image_l_url", "23": "image_s_url",
	"24": "info_url", "25": "end_time", "26": "gifts", "27": "fate_player_number",
	"28": "expensive_price", "29": "is_odds_view", "30": "stepup_count", "31": "is_last_step",
}

var cnUserInfoFieldMap = map[string]string{
	"0": "userid", "1": "name", "2": "active_arthur_type", "3": "arthur_rank",
	"4": "lv", "5": "exp", "6": "now_lv_exp", "7": "next_lv_exp", "8": "fame",
	"9": "leader_card_uniqid", "10": "leader_cardid", "11": "comment", "12": "ap",
	"13": "ap_max", "14": "ap_next_sec", "15": "ap_heal_sec", "16": "bp",
	"17": "bp_max", "18": "bp_next_sec", "19": "bp_heal_sec", "20": "card_max_extend",
	"21": "card_num", "22": "card_max", "23": "card_extend_limit", "24": "card_container_num",
	"25": "card_container_max_extend", "26": "card_container_max", "27": "card_container_extend_limit",
	"28": "sphr_num", "29": "sphr_max", "30": "follow_max", "31": "friend_max_extend",
	"32": "friend_extend_limit", "33": "gold", "34": "fp", "35": "coin", "36": "coin_free",
	"37": "enter_state", "38": "navi_type", "39": "inviteid", "40": "jobs",
	"41": "deck_max_per_arthur", "42": "tag_name", "43": "total_cost",
	"44": "support_deck_set_card_num", "45": "rookie_type", "46": "fresher_left_time",
	"47": "rookie_point", "48": "punished_free_point", "49": "punished_free_point_max",
	"50": "punished_free_point_next_sec", "51": "punished_free_point_heal_sec",
	"52": "buddy_num", "53": "buddy_max",
}

var cnJobInfoFieldMap = map[string]string{
	"0": "hp", "1": "atkp", "2": "intp", "3": "mndp",
}

var cnRewardInfoFieldMap = map[string]string{
	"0": "type", "1": "num", "2": "reward_typeid", "3": "card_lv",
	"4": "card_fame", "5": "card_love", "6": "card_skill_lv",
}

var cnItemInfoFieldMap = map[string]string{
	"0": "itemid", "1": "num", "2": "limit_time", "3": "exchange",
}

func cnBootstrapGachaShow(businessHandler http.Handler, operations *operationservice.Runtime) http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		payload, err := readCNSessionPayload(request, "GachaShow")
		if err != nil {
			http.Error(writer, err.Error(), http.StatusUnauthorized)
			return
		}
		var fields map[string]json.RawMessage
		var showType int
		if json.Unmarshal(payload, &fields) != nil || len(fields) != 1 ||
			json.Unmarshal(fields["0"], &showType) != nil {
			http.Error(writer, "invalid CN GachaShow payload", http.StatusBadRequest)
			return
		}
		domainBody, _ := json.Marshal(map[string]int{"show_type": showType})
		active, err := operations.GachaPublication()
		if err != nil {
			http.Error(writer, "read local gacha publication", http.StatusInternalServerError)
			return
		}
		forwardCNBusiness(
			writer,
			adaptCNBusinessRequest(request, http.MethodPost, "/GachaShow", domainBody),
			businessHandler,
			func(content []byte) ([]byte, error) {
				return adaptCNGachaShowPublicationResponse(
					content, active, operations.ManagedGroups(),
				)
			},
		)
	}
}

func cnBootstrapGachaPlay(businessHandler http.Handler, operations *operationservice.Runtime) http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		payload, err := readCNSessionPayload(request, "GachaPlay2")
		if err != nil {
			http.Error(writer, err.Error(), http.StatusUnauthorized)
			return
		}
		var fields map[string]json.RawMessage
		if json.Unmarshal(payload, &fields) != nil || (len(fields) != 4 && len(fields) != 5) {
			http.Error(writer, "invalid CN GachaPlay2 payload", http.StatusBadRequest)
			return
		}
		var gachaID, payType, popupID int
		var gachaHash string
		if json.Unmarshal(fields["0"], &gachaID) != nil ||
			json.Unmarshal(fields["1"], &payType) != nil ||
			json.Unmarshal(fields["2"], &gachaHash) != nil ||
			json.Unmarshal(fields["4"], &popupID) != nil {
			http.Error(writer, "invalid CN GachaPlay2 fields", http.StatusBadRequest)
			return
		}
		for key := range fields {
			if key != "0" && key != "1" && key != "2" && key != "3" && key != "4" {
				http.Error(writer, "unexpected CN GachaPlay2 field", http.StatusBadRequest)
				return
			}
		}
		selectLineup, err := decodeCNGachaSelectLineup(fields["3"])
		if err != nil {
			http.Error(writer, "invalid CN GachaPlay2 select lineup", http.StatusBadRequest)
			return
		}
		active, err := operations.GachaPublication()
		if err != nil {
			http.Error(writer, "read local gacha publication", http.StatusInternalServerError)
			return
		}
		request = httpapi.WithGachaPublication(request, func(id int) bool { return operations.GachaIDPublished(id, active) })
		domainBody, _ := json.Marshal(map[string]any{
			"gachaid": gachaID, "pay_type": payType, "gacha_hash": gachaHash,
			"select_lineup_list": selectLineup, "popupid": popupID,
		})
		forwardCNBusiness(
			writer,
			adaptCNBusinessRequest(request, http.MethodPost, "/GachaPlay2", domainBody),
			businessHandler,
			func(content []byte) ([]byte, error) {
				return adaptCNGachaPlayPublicationResponse(
					content, active, operations.ManagedGroups(),
				)
			},
		)
	}
}

func decodeCNGachaSelectLineup(raw json.RawMessage) ([]map[string]json.RawMessage, error) {
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return []map[string]json.RawMessage{}, nil
	}
	var source []map[string]json.RawMessage
	if err := json.Unmarshal(raw, &source); err != nil {
		return nil, err
	}
	fieldNames := []string{
		"type", "num", "reward_typeid", "card_lv", "card_fame", "card_love", "card_skill_lv",
	}
	result := make([]map[string]json.RawMessage, len(source))
	for index, entry := range source {
		if len(entry) != len(fieldNames) {
			return nil, errors.New("CN gacha select reward fields differ")
		}
		mapped := make(map[string]json.RawMessage, len(fieldNames))
		for fieldIndex, fieldName := range fieldNames {
			value, exists := entry[string(rune('0'+fieldIndex))]
			if !exists {
				return nil, errors.New("CN gacha select reward field is missing")
			}
			mapped[fieldName] = value
		}
		result[index] = mapped
	}
	return result, nil
}

func cnBootstrapGachaLineupShow(businessHandler http.Handler, operations *operationservice.Runtime) http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		payload, err := readCNSessionPayload(request, "GachaLineupShow2")
		if err != nil {
			http.Error(writer, err.Error(), http.StatusUnauthorized)
			return
		}
		var fields map[string]json.RawMessage
		var gachaIDs []int
		var popupID int
		if json.Unmarshal(payload, &fields) != nil || len(fields) != 2 ||
			json.Unmarshal(fields["gachaids"], &gachaIDs) != nil ||
			json.Unmarshal(fields["popupid"], &popupID) != nil {
			http.Error(writer, "invalid CN GachaLineupShow2 payload", http.StatusBadRequest)
			return
		}
		domainBody, _ := json.Marshal(map[string]any{"gachaids": gachaIDs, "popupid": popupID})
		active, err := operations.GachaPublication()
		if err != nil {
			http.Error(writer, "read local gacha publication", http.StatusInternalServerError)
			return
		}
		request = httpapi.WithGachaPublication(request, func(id int) bool { return operations.GachaIDPublished(id, active) })
		forwardCNBusiness(
			writer,
			adaptCNBusinessRequest(request, http.MethodPost, "/GachaLineupShow2", domainBody),
			businessHandler,
			adaptCNGachaLineupShowResponse,
		)
	}
}

func cnBootstrapGachaOddsShow(businessHandler http.Handler, operations *operationservice.Runtime) http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		payload, err := readCNSessionPayload(request, "GachaOddsShow")
		if err != nil {
			http.Error(writer, err.Error(), http.StatusUnauthorized)
			return
		}
		var fields map[string]json.RawMessage
		var gachaID, popupID int
		if json.Unmarshal(payload, &fields) != nil || len(fields) != 2 ||
			json.Unmarshal(fields["gachaid"], &gachaID) != nil ||
			json.Unmarshal(fields["popupid"], &popupID) != nil || gachaID <= 0 {
			http.Error(writer, "invalid CN GachaOddsShow payload", http.StatusBadRequest)
			return
		}
		domainBody, _ := json.Marshal(map[string]int{"gachaid": gachaID, "popupid": popupID})
		active, err := operations.GachaPublication()
		if err != nil {
			http.Error(writer, "read local gacha publication", http.StatusInternalServerError)
			return
		}
		request = httpapi.WithGachaPublication(request, func(id int) bool { return operations.GachaIDPublished(id, active) })
		forwardCNBusiness(
			writer,
			adaptCNBusinessRequest(request, http.MethodPost, "/GachaOddsShow", domainBody),
			businessHandler,
			adaptCNGachaOddsShowResponse,
		)
	}
}

func adaptCNGachaShowPublicationResponse(
	content []byte,
	active map[int]struct{},
	managed map[int]struct{},
) ([]byte, error) {
	return adaptCNProtocolMethod(content, func(method map[string]json.RawMessage) (any, error) {
		if managed != nil {
			if err := filterCNGachaPublication(method, active, managed); err != nil {
				return nil, err
			}
		}
		categories, err := remapJSONArray(method["gacha_category_list"], cnGachaCategoryFieldMap)
		if err != nil {
			return nil, err
		}
		gachas, err := remapCNGachaInfos(method["gacha_list"])
		if err != nil {
			return nil, err
		}
		user, err := remapCNUserInfo(method["user"])
		if err != nil {
			return nil, err
		}
		return map[string]any{
			"0": categories,
			"1": gachas,
			"2": rawOrZero(method["is_valid_lineup_cache"]),
			"3": user,
		}, nil
	})
}

func adaptCNGachaItemPlayMethod(method map[string]json.RawMessage) (any, error) {
	for field, fields := range map[string]map[string]string{
		"cards": cnCardInfoNamedFieldMap, "stack_cards": cnCardStackInfoNamedFieldMap,
		"sphrs": cnSphereInfoNamedFieldMap, "buddys": cnBuddyInfoNamedFieldMap,
	} {
		entries, err := remapJSONArray(method[field], fields)
		if err != nil {
			return nil, err
		}
		method[field], err = json.Marshal(entries)
		if err != nil {
			return nil, err
		}
	}
	return method, nil
}

func adaptCNGachaPlayPublicationResponse(
	content []byte,
	active map[int]struct{},
	managed map[int]struct{},
) ([]byte, error) {
	return adaptCNProtocolMethod(content, func(method map[string]json.RawMessage) (any, error) {
		if managed != nil {
			if err := filterCNGachaList(method, active, managed); err != nil {
				return nil, err
			}
		}
		cards, err := remapJSONArray(method["cards"], cnCardInfoFieldMap)
		if err != nil {
			return nil, err
		}
		user, err := remapCNUserInfo(method["user"])
		if err != nil {
			return nil, err
		}
		item, err := remapCNItemInfo(method["item"])
		if err != nil {
			return nil, err
		}
		gachas, err := remapCNGachaInfos(method["gacha_list"])
		if err != nil {
			return nil, err
		}
		rewards, err := remapCNGachaRewards(method["rewards"])
		if err != nil {
			return nil, err
		}
		items, err := remapCNItemInfos(method["new_items"])
		if err != nil {
			return nil, err
		}
		gifts, err := remapJSONArray(method["gift_direct"], cnRewardInfoFieldMap)
		if err != nil {
			return nil, err
		}
		presents, err := remapJSONArray(method["gift_present"], cnRewardInfoFieldMap)
		if err != nil {
			return nil, err
		}
		return map[string]any{
			"0":  cards,
			"1":  rawOrEmptyArray(method["stack_cards"]),
			"2":  rawOrEmptyArray(method["sphrs"]),
			"3":  items,
			"4":  rawOrEmptyArray(method["buddys"]),
			"5":  user,
			"6":  item,
			"7":  gachas,
			"8":  gifts,
			"9":  presents,
			"10": rewards,
			"11": rawOrEmptyArray(method["reward_adds"]),
			"12": rawOrZero(method["expectancy"]),
			"13": rawOrEmptyArray(method["cutin"]),
			"14": rawOrEmptyArray(method["auto_fusion_result"]),
			"15": rawOrEmptyArray(method["auto_loveup_result"]),
		}, nil
	})
}

func filterCNGachaPublication(
	method map[string]json.RawMessage,
	active map[int]struct{},
	managed map[int]struct{},
) error {
	if err := filterCNGachaList(method, active, managed); err != nil {
		return err
	}
	var gachas []struct {
		CategoryNum int `json:"category_num"`
	}
	if err := json.Unmarshal(method["gacha_list"], &gachas); err != nil {
		return err
	}
	visibleCategories := make(map[int]struct{}, len(gachas))
	for _, gacha := range gachas {
		visibleCategories[gacha.CategoryNum] = struct{}{}
	}
	var categories []json.RawMessage
	if err := json.Unmarshal(method["gacha_category_list"], &categories); err != nil {
		return err
	}
	filtered := make([]json.RawMessage, 0, len(categories))
	for _, raw := range categories {
		var category struct {
			CategoryNum int `json:"category_num"`
		}
		if err := json.Unmarshal(raw, &category); err != nil {
			return err
		}
		if _, exists := visibleCategories[category.CategoryNum]; exists {
			filtered = append(filtered, raw)
		}
	}
	updated, err := json.Marshal(filtered)
	if err != nil {
		return err
	}
	method["gacha_category_list"] = updated
	return nil
}

func filterCNGachaList(
	method map[string]json.RawMessage,
	active map[int]struct{},
	managed map[int]struct{},
) error {
	var gachas []json.RawMessage
	if err := json.Unmarshal(method["gacha_list"], &gachas); err != nil {
		return err
	}
	filtered := make([]json.RawMessage, 0, len(gachas))
	for _, raw := range gachas {
		var identity struct {
			GroupID int `json:"groupid"`
		}
		if err := json.Unmarshal(raw, &identity); err != nil {
			return err
		}
		if _, isManaged := managed[identity.GroupID]; isManaged {
			if _, published := active[identity.GroupID]; !published {
				continue
			}
		}
		filtered = append(filtered, raw)
	}
	updated, err := json.Marshal(filtered)
	if err != nil {
		return err
	}
	method["gacha_list"] = updated
	return nil
}

func adaptCNGachaLineupShowResponse(content []byte) ([]byte, error) {
	return adaptCNProtocolMethod(content, func(method map[string]json.RawMessage) (any, error) {
		// Unlike GachaShow and GachaPlay2, the CN 6.0.2 generated receiver
		// consumes this DTO by its named fields all the way down through prize.
		return map[string]any{"gachas": rawOrEmptyArray(method["gachas"])}, nil
	})
}

func adaptCNGachaOddsShowResponse(content []byte) ([]byte, error) {
	return adaptCNProtocolMethod(content, func(method map[string]json.RawMessage) (any, error) {
		// The CN 6.0.2 generated receiver consumes this DTO and its nested
		// RewardInfo objects by named fields rather than ProtoGen numeric slots.
		return map[string]any{
			"odds_msg":     method["odds_msg"],
			"lineup_infos": rawOrEmptyArray(method["lineup_infos"]),
		}, nil
	})
}

func remapCNUserInfo(raw json.RawMessage) (any, error) {
	var user map[string]json.RawMessage
	if err := json.Unmarshal(raw, &user); err != nil {
		return nil, err
	}
	jobs, err := remapJSONArray(user["jobs"], cnJobInfoFieldMap)
	if err != nil {
		return nil, err
	}
	jobsJSON, err := json.Marshal(jobs)
	if err != nil {
		return nil, err
	}
	user["jobs"] = jobsJSON
	updated, err := json.Marshal(user)
	if err != nil {
		return nil, err
	}
	mapped, err := remapJSONObject(updated, cnUserInfoFieldMap)
	if err != nil {
		return nil, err
	}
	// Modern client fields use their names in both wire forms. Keep the CN
	// slots stable: slot 42 is still tag_name, not the JP alchemist limit.
	if value, ok := user["deck_max_alchemist"]; ok {
		mapped["deck_max_alchemist"] = value
	}
	return mapped, nil
}

func remapCNItemInfo(raw json.RawMessage) (any, error) {
	var item map[string]json.RawMessage
	if err := json.Unmarshal(raw, &item); err != nil {
		return nil, err
	}
	var exchange map[string]json.RawMessage
	if err := json.Unmarshal(item["exchange"], &exchange); err != nil {
		return nil, err
	}
	reward, err := remapJSONObject(exchange["reward"], cnRewardInfoFieldMap)
	if err != nil {
		return nil, err
	}
	exchangeValue := map[string]any{
		"0": rawOrZero(exchange["is_appear_event"]),
		"1": rawOrZero(exchange["eventid"]),
		"2": rawOrZero(exchange["need_num"]),
		"3": reward,
	}
	exchangeJSON, err := json.Marshal(exchangeValue)
	if err != nil {
		return nil, err
	}
	item["exchange"] = exchangeJSON
	updated, err := json.Marshal(item)
	if err != nil {
		return nil, err
	}
	return remapJSONObject(updated, cnItemInfoFieldMap)
}

func remapCNItemInfos(raw json.RawMessage) ([]any, error) {
	var source []json.RawMessage
	if err := json.Unmarshal(raw, &source); err != nil {
		return nil, err
	}
	result := make([]any, len(source))
	for i, item := range source {
		var err error
		result[i], err = remapCNItemInfo(item)
		if err != nil {
			return nil, err
		}
	}
	return result, nil
}

// GachaShow/Play2 use numeric keys for both nested gift wrappers and RewardInfo.
func remapCNGachaInfos(raw json.RawMessage) ([]any, error) {
	var source []map[string]json.RawMessage
	if err := json.Unmarshal(raw, &source); err != nil {
		return nil, err
	}
	result := make([]any, len(source))
	for i, gacha := range source {
		var gifts []map[string]json.RawMessage
		if err := json.Unmarshal(gacha["gifts"], &gifts); err != nil {
			return nil, err
		}
		mapped := make([]any, len(gifts))
		for j, gift := range gifts {
			var entries []map[string]json.RawMessage
			if err := json.Unmarshal(gift["rewards"], &entries); err != nil {
				return nil, err
			}
			rewards := make([]any, len(entries))
			for k, entry := range entries {
				reward, err := remapJSONObject(entry["reward"], cnRewardInfoFieldMap)
				if err != nil {
					return nil, err
				}
				rewards[k] = map[string]any{"0": rawOrZero(entry["is_empty"]), "1": reward}
			}
			mapped[j] = map[string]any{"0": rawOrZero(gift["is_random"]), "1": rewards}
		}
		gacha["gifts"], _ = json.Marshal(mapped)
		body, err := json.Marshal(gacha)
		if err != nil {
			return nil, err
		}
		result[i], err = remapJSONObject(body, cnGachaInfoFieldMap)
		if err != nil {
			return nil, err
		}
	}
	return result, nil
}

func remapCNGachaRewards(raw json.RawMessage) ([]any, error) {
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return []any{}, nil
	}
	var source []map[string]json.RawMessage
	if err := json.Unmarshal(raw, &source); err != nil {
		return nil, err
	}
	result := make([]any, len(source))
	for index, received := range source {
		reward, err := remapJSONObject(received["reward"], cnRewardInfoFieldMap)
		if err != nil {
			return nil, err
		}
		if len(received["uniqid"]) == 0 {
			return nil, errors.New("CN gacha reward has no unique IDs")
		}
		result[index] = map[string]any{
			"0": reward,
			"1": received["uniqid"],
			"2": rawOrZero(received["is_new"]),
			"3": rawOrZero(received["auto_fusion_used"]),
			"4": rawOrZero(received["auto_loveup_used"]),
			"5": rawOrEmptyArray(received["add"]),
		}
	}
	return result, nil
}
