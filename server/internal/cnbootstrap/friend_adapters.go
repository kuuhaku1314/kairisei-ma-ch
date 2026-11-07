package cnbootstrap

import (
	"encoding/json"
	"net/http"
)

var cnFriendUserInfoFieldMap = map[string]string{
	"userid":            "userid",
	"name":              "name",
	"arthur_type":       "arthur_type",
	"is_burst":          "is_burst",
	"lv":                "lv",
	"deck_rank":         "deck_rank",
	"state":             "state",
	"leader_cardid":     "leader_cardid",
	"leader_card_lv":    "leader_card_lv",
	"leader_card_fame":  "leader_card_fame",
	"last_login_time":   "last_login_time",
	"comment":           "comment",
	"pvp_point":         "pvp_point",
	"is_first_matching": "is_first_matching",
	"rookie_type":       "is_rookie_bonus",
	"deck_honorids":     "deck_honorids",
}

func cnBootstrapHonorShow(businessHandler http.Handler) http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		if err := requireCNSessionOnly(request, "HonorShow"); err != nil {
			http.Error(writer, err.Error(), http.StatusUnauthorized)
			return
		}
		forwardCNBusiness(
			writer,
			adaptCNBusinessRequest(request, http.MethodPost, "/HonorShow", nil),
			businessHandler,
			func(content []byte) ([]byte, error) { return content, nil },
		)
	}
}

func cnBootstrapFollowShow(businessHandler http.Handler) http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		if err := requireCNSessionOnly(request, "FollowoFollowShow"); err != nil {
			http.Error(writer, err.Error(), http.StatusUnauthorized)
			return
		}
		forwardCNBusiness(
			writer,
			adaptCNBusinessRequest(request, http.MethodPost, "/FollowoFollowShow", nil),
			businessHandler,
			func(content []byte) ([]byte, error) {
				return adaptCNProtocolMethod(content, func(method map[string]json.RawMessage) (any, error) {
					friends, err := remapJSONArray(method["friends"], cnFriendUserInfoFieldMap)
					if err != nil {
						return nil, err
					}
					return map[string]any{
						"friends": friends,
					}, nil
				})
			},
		)
	}
}

func cnBootstrapFollowerShow(businessHandler http.Handler) http.HandlerFunc {
	return cnPayloadAdapter(
		businessHandler,
		"FollowFollowerShow",
		"/FollowFollowerShow",
		[]string{"page"},
		func(method map[string]json.RawMessage) (any, error) {
			friends, err := remapJSONArray(method["friends"], cnFriendUserInfoFieldMap)
			if err != nil {
				return nil, err
			}
			return map[string]any{
				"friends":      friends,
				"follower_num": rawOrZero(method["follower_num"]),
				"page_max":     rawOrZero(method["page_max"]),
			}, nil
		},
	)
}

func cnBootstrapFriendSearch(businessHandler http.Handler) http.HandlerFunc {
	return cnPayloadAdapter(
		businessHandler,
		"FriendSearch",
		"/FriendSearch",
		[]string{"inviteid"},
		func(method map[string]json.RawMessage) (any, error) {
			users, err := remapJSONArray(method["user"], cnFriendUserInfoFieldMap)
			if err != nil {
				return nil, err
			}
			return map[string]any{"user": users}, nil
		},
	)
}

func cnBootstrapFollowAdd(businessHandler http.Handler) http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		payload, err := normalizeCNOptionalFields(request, "FollowAdd", []string{"userids"}, []string{"userids"})
		if err != nil {
			http.Error(writer, err.Error(), http.StatusBadRequest)
			return
		}
		var requestData struct {
			UserIDs []int `json:"userids"`
		}
		if err := json.Unmarshal(payload, &requestData); err != nil {
			http.Error(writer, "invalid CN FollowAdd payload", http.StatusBadRequest)
			return
		}
		// CN 6.0.2 always sends the ten visible search-result slots. Empty
		// slots are serialized as zero and are not follow targets.
		selectedUserIDs := make([]int, 0, len(requestData.UserIDs))
		for _, userID := range requestData.UserIDs {
			if userID != 0 {
				selectedUserIDs = append(selectedUserIDs, userID)
			}
		}
		payload, err = json.Marshal(map[string]any{"userids": selectedUserIDs})
		if err != nil {
			http.Error(writer, "normalize CN FollowAdd payload", http.StatusInternalServerError)
			return
		}
		forwardCNBusiness(
			writer,
			adaptCNBusinessRequest(request, http.MethodPost, "/FollowAdd", payload),
			businessHandler,
			func(content []byte) ([]byte, error) {
				return adaptCNProtocolMethod(content, func(method map[string]json.RawMessage) (any, error) {
					return map[string]any{
						"request_userids": rawOrEmptyArray(method["request_userids"]),
						"is_friend_full":  rawOrZero(method["is_friend_full"]),
					}, nil
				})
			},
		)
	}
}

func cnBootstrapFollowUnfollow(businessHandler http.Handler) http.HandlerFunc {
	return cnPayloadAdapter(
		businessHandler,
		"FollowUnFollow",
		"/FollowUnFollow",
		[]string{"userid"},
		func(map[string]json.RawMessage) (any, error) {
			return map[string]any{}, nil
		},
	)
}

func cnBootstrapUserProfileShow(businessHandler http.Handler) http.HandlerFunc {
	return cnPayloadAdapter(
		businessHandler,
		"UserProfileShow",
		"/UserProfileShow",
		[]string{"userid"},
		func(method map[string]json.RawMessage) (any, error) {
			return map[string]any{"profile": method["profile"]}, nil
		},
	)
}

func cnPayloadAdapter(
	businessHandler http.Handler,
	operation string,
	requestPath string,
	fields []string,
	adapt func(map[string]json.RawMessage) (any, error),
) http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		payload, err := normalizeCNOptionalFields(request, operation, fields, fields)
		if err != nil {
			http.Error(writer, err.Error(), http.StatusBadRequest)
			return
		}
		forwardCNBusiness(
			writer,
			adaptCNBusinessRequest(request, http.MethodPost, requestPath, payload),
			businessHandler,
			func(content []byte) ([]byte, error) {
				return adaptCNProtocolMethod(content, adapt)
			},
		)
	}
}
