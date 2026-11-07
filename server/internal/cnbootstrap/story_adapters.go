package cnbootstrap

import (
	"encoding/json"
	"net/http"
)

func cnBootstrapStoryTeamBattleEnd(businessHandler http.Handler) http.HandlerFunc {
	return cnPayloadAdapter(businessHandler, "StoryTeamBattleEnd", "/StoryTeamBattleEnd", []string{},
		func(method map[string]json.RawMessage) (any, error) {
			if err := remapCNNamedRewardArrays(method); err != nil {
				return nil, err
			}
			var unlocks []map[string]json.RawMessage
			if len(method["burst_unlock"]) != 0 {
				if err := json.Unmarshal(method["burst_unlock"], &unlocks); err != nil {
					return nil, err
				}
			}
			if unlocks == nil {
				unlocks = []map[string]json.RawMessage{}
			}
			for _, unlock := range unlocks {
				if err := remapCNNamedRewardArrays(unlock); err != nil {
					return nil, err
				}
				decks, err := remapJSONArray(unlock["decks"], cnDeckInfoNamedFieldMap)
				if err != nil {
					return nil, err
				}
				unlock["decks"], err = json.Marshal(decks)
				if err != nil {
					return nil, err
				}
			}
			var err error
			method["burst_unlock"], err = json.Marshal(unlocks)
			return method, err
		})
}

func cnBootstrapStoryMainShow(businessHandler http.Handler) http.HandlerFunc {
	return cnPayloadAdapter(
		businessHandler,
		"StoryMainShow",
		"/StoryMainShow",
		[]string{"cn_story"},
		func(method map[string]json.RawMessage) (any, error) {
			return map[string]any{"parts": rawOrEmptyArray(method["parts"])}, nil
		},
	)
}

func cnBootstrapStoryMainStart(businessHandler http.Handler) http.HandlerFunc {
	return cnPayloadAdapter(
		businessHandler,
		"StoryMainStart",
		"/StoryMainStart",
		[]string{"story_mainid"},
		func(method map[string]json.RawMessage) (any, error) {
			return map[string]any{
				"seed":                rawOrZero(method["seed"]),
				"hp":                  rawOrZero(method["hp"]),
				"hp_max":              rawOrZero(method["hp_max"]),
				"cost_initial":        rawOrZero(method["cost_initial"]),
				"burst_gauge_initial": rawOrZero(method["burst_gauge_initial"]),
				"hold_max":            rawOrZero(method["hold_max"]),
			}, nil
		},
	)
}

func cnBootstrapStoryMainEnd(businessHandler http.Handler) http.HandlerFunc {
	return cnPayloadAdapter(
		businessHandler,
		"StoryMainEnd",
		"/StoryMainEnd",
		[]string{"is_clear"},
		func(method map[string]json.RawMessage) (any, error) {
			if err := remapCNNamedRewardArrays(method); err != nil {
				return nil, err
			}
			return map[string]any{
				"user":                           method["user"],
				"clear_rewards":                  rawOrEmptyArray(method["clear_rewards"]),
				"new_cards":                      rawOrEmptyArray(method["new_cards"]),
				"new_stack_cards":                rawOrEmptyArray(method["new_stack_cards"]),
				"new_items":                      rawOrEmptyArray(method["new_items"]),
				"new_sphrs":                      rawOrEmptyArray(method["new_sphrs"]),
				"new_buddys":                     rawOrEmptyArray(method["new_buddys"]),
				"is_clear_reward_in_present_box": rawOrZero(method["is_clear_reward_in_present_box"]),
				"unlock_notice":                  rawOrEmptyArray(method["unlock_notice"]),
			}, nil
		},
	)
}

func cnBootstrapStorySubShow(businessHandler http.Handler) http.HandlerFunc {
	return cnPayloadAdapter(
		businessHandler,
		"StorySubShow",
		"/StorySubShow",
		[]string{"cn_story"},
		func(method map[string]json.RawMessage) (any, error) {
			return map[string]any{"characters": rawOrEmptyArray(method["characters"])}, nil
		},
	)
}

func cnBootstrapStorySubStart(businessHandler http.Handler) http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		// Modern tower reads add towerid/floor. The business decoder owns the
		// paired field contract; ordinary CN sub-story requests still have one ID.
		payload, err := readCNSessionPayload(request, "StorySubStart")
		if err != nil {
			http.Error(writer, err.Error(), http.StatusUnauthorized)
			return
		}
		forwardCNBusiness(writer, adaptCNBusinessRequest(request, http.MethodPost, "/StorySubStart", payload), businessHandler,
			func(content []byte) ([]byte, error) {
				return adaptCNProtocolMethod(content, func(method map[string]json.RawMessage) (any, error) {
					return map[string]any{
						"seed":                rawOrZero(method["seed"]),
						"hp":                  rawOrZero(method["hp"]),
						"hp_max":              rawOrZero(method["hp_max"]),
						"cost_initial":        rawOrZero(method["cost_initial"]),
						"burst_gauge_initial": rawOrZero(method["burst_gauge_initial"]),
						"hold_max":            rawOrZero(method["hold_max"]),
					}, nil
				})
			})
	}
}

func cnBootstrapStorySubEnd(businessHandler http.Handler) http.HandlerFunc {
	return cnPayloadAdapter(
		businessHandler,
		"StorySubEnd",
		"/StorySubEnd",
		[]string{"is_clear"},
		func(method map[string]json.RawMessage) (any, error) {
			if err := remapCNNamedRewardArrays(method); err != nil {
				return nil, err
			}
			return map[string]any{
				"user":                           method["user"],
				"clear_rewards":                  rawOrEmptyArray(method["clear_rewards"]),
				"new_cards":                      rawOrEmptyArray(method["new_cards"]),
				"new_stack_cards":                rawOrEmptyArray(method["new_stack_cards"]),
				"new_items":                      rawOrEmptyArray(method["new_items"]),
				"new_sphrs":                      rawOrEmptyArray(method["new_sphrs"]),
				"new_buddys":                     rawOrEmptyArray(method["new_buddys"]),
				"is_clear_reward_in_present_box": rawOrZero(method["is_clear_reward_in_present_box"]),
				"unlock_notice":                  rawOrEmptyArray(method["unlock_notice"]),
			}, nil
		},
	)
}

func cnBootstrapStoryEventShow(businessHandler http.Handler) http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		if err := requireCNSessionOnly(request, "StoryEventShow"); err != nil {
			http.Error(writer, err.Error(), http.StatusUnauthorized)
			return
		}
		forwardCNBusiness(
			writer,
			adaptCNBusinessRequest(request, http.MethodPost, "/StoryEventShow", nil),
			businessHandler,
			func(content []byte) ([]byte, error) {
				return adaptCNProtocolMethod(content, func(method map[string]json.RawMessage) (any, error) {
					return map[string]any{"events": rawOrEmptyArray(method["events"])}, nil
				})
			},
		)
	}
}

func cnBootstrapStoryEventUnlock(businessHandler http.Handler) http.HandlerFunc {
	return cnPayloadAdapter(
		businessHandler,
		"StoryEventUnlock",
		"/StoryEventUnlock",
		[]string{"story_subid"},
		func(method map[string]json.RawMessage) (any, error) {
			return map[string]any{
				"user":   method["user"],
				"items":  rawOrEmptyArray(method["items"]),
				"events": rawOrEmptyArray(method["events"]),
			}, nil
		},
	)
}

func cnBootstrapStoryStart(businessHandler http.Handler) http.HandlerFunc {
	return cnPayloadAdapter(
		businessHandler,
		"StoryStart",
		"/StoryStart",
		[]string{"story_battleid", "deck_arthur_type", "deck_arthur_type_idxs"},
		func(_ map[string]json.RawMessage) (any, error) {
			return map[string]any{}, nil
		},
	)
}
