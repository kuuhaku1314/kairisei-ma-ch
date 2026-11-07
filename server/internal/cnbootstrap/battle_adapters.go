package cnbootstrap

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"

	operationservice "kairisei.local/server/internal/operations"
)

// The business decoder owns the current request contract.
func cnBootstrapSoloStart(businessHandler http.Handler) http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		payload, err := readCNSessionPayload(request, "TeamBattleSoloStart")
		if err != nil {
			http.Error(writer, err.Error(), http.StatusUnauthorized)
			return
		}
		businessHandler.ServeHTTP(writer, adaptCNBusinessRequest(request, http.MethodPost, "/TeamBattleSoloStart", payload))
	}
}

func cnBootstrapTeamBattleSoloShow(businessHandler http.Handler, operations *operationservice.Runtime) http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		payload, err := readCNSessionPayload(request, "TeamBattleSoloShow")
		if err != nil {
			http.Error(writer, err.Error(), http.StatusUnauthorized)
			return
		}
		if err := requireCNExactFields(payload, "TeamBattleSoloShow", "0"); err != nil {
			http.Error(writer, err.Error(), http.StatusBadRequest)
			return
		}
		var wire map[string]int8
		if err := json.Unmarshal(payload, &wire); err != nil {
			http.Error(writer, "invalid CN TeamBattleSoloShow payload", http.StatusBadRequest)
			return
		}
		activeArthurType := wire["0"]
		if activeArthurType < 1 || activeArthurType > 5 {
			http.Error(writer, "invalid CN TeamBattleSoloShow Arthur type", http.StatusBadRequest)
			return
		}
		adaptedPayload, err := json.Marshal(map[string]int8{"active_arthur_type": activeArthurType})
		if err != nil {
			http.Error(writer, "encode TeamBattleSoloShow adapter", http.StatusInternalServerError)
			return
		}
		allowed, err := operations.TeamBattleGroupAllowlist()
		if err != nil {
			http.Error(writer, "read local team battle publication", http.StatusInternalServerError)
			return
		}
		forwardCNBusiness(
			writer,
			adaptCNBusinessRequest(request, http.MethodPost, "/TeamBattleSoloShow", adaptedPayload),
			businessHandler,
			func(content []byte) ([]byte, error) {
				return adaptCNTeamBattleSoloShowPublicationResponse(content, allowed)
			},
		)
	}
}

func cnBootstrapPastBossShow(businessHandler http.Handler, operations *operationservice.Runtime) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := requireCNSessionOnly(r, "TeamBattlePastBossShow"); err != nil {
			http.Error(w, err.Error(), http.StatusUnauthorized)
			return
		}
		allowed, err := operations.BattleGroupAllowlist(operationservice.PastBattlePublicationKey)
		if err != nil {
			http.Error(w, "read local past boss publication", http.StatusInternalServerError)
			return
		}
		forwardCNBusiness(w, adaptCNBusinessRequest(r, http.MethodPost, "/TeamBattlePastBossShow", nil), businessHandler, func(content []byte) ([]byte, error) {
			return adaptCNProtocolMethod(content, func(method map[string]json.RawMessage) (any, error) {
				if allowed != nil {
					groups, err := filterCNTeamBattleGroups(method["0"], allowed)
					if err != nil {
						return nil, err
					}
					method["0"], err = json.Marshal(groups)
					if err != nil {
						return nil, err
					}
				}
				return method, nil
			})
		})
	}
}

func cnBootstrapTeamBattleResult(businessHandler http.Handler) http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		payload, err := readCNSessionPayload(request, "TeamBattleResult")
		if err != nil {
			http.Error(writer, err.Error(), http.StatusUnauthorized)
			return
		}
		forwardCNBusiness(
			writer,
			adaptCNBusinessRequest(request, http.MethodPost, "/TeamBattleResult", payload),
			businessHandler,
			adaptCNTeamBattleResultResponse,
		)
	}
}

func cnBootstrapTeamBattleSoloEnd(businessHandler http.Handler) http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		payload, err := readCNSessionPayload(request, "TeamBattleSoloEnd")
		if err != nil {
			http.Error(writer, err.Error(), http.StatusUnauthorized)
			return
		}
		forwardCNBusiness(
			writer,
			adaptCNBusinessRequest(request, http.MethodPost, "/TeamBattleSoloEnd", payload),
			businessHandler,
			adaptCNTeamBattleResultResponse,
		)
	}
}

// Permanent report validation belongs to the use case so its protocol result
// reaches the client's cache-discard callback instead of becoming HTTP 400.
func cnBootstrapPvpEnd(business http.Handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		payload, err := readCNSessionPayload(r, "PvpEnd")
		if err != nil {
			http.Error(w, err.Error(), http.StatusUnauthorized)
			return
		}
		business.ServeHTTP(w, adaptCNBusinessRequest(r, http.MethodPost, "/PvpEnd", payload))
	}
}

func adaptCNTeamBattleResultResponse(content []byte) ([]byte, error) {
	return adaptCNProtocolMethod(content, func(method map[string]json.RawMessage) (any, error) {
		if err := remapCNNamedRewardArrays(method); err != nil {
			return nil, err
		}
		return method, nil
	})
}

func adaptCNTeamBattleSoloShowPublicationResponse(content []byte, allowed map[int]struct{}) ([]byte, error) {
	return adaptCNProtocolMethod(content, func(method map[string]json.RawMessage) (any, error) {
		normalRaw, normalExists := method["9"]
		if !normalExists || len(normalRaw) == 0 || bytes.Equal(normalRaw, []byte("null")) {
			return nil, errors.New("CN TeamBattleSoloShow response has no local training group")
		}

		// The domain projection keeps normal, 3D activity, key and 2D activity
		// lists distinct. Do not duplicate the normal list into empty categories:
		// the original client treats each array as a separate tab and duplicated
		// data destroys both tab identity and account-scoped normal progression.
		for _, field := range []string{"10", "11", "12"} {
			var groups []json.RawMessage
			groupsRaw, exists := method[field]
			if !exists {
				return nil, errors.New("CN TeamBattleSoloShow response has no boss category " + field)
			}
			if err := json.Unmarshal(groupsRaw, &groups); err != nil {
				return nil, err
			}
		}
		if allowed != nil {
			// Field 9 is normal quest progression, not the operations BOSS
			// catalog. Limiting activities must not hide the player's next area.
			for _, field := range []string{"10", "11", "12"} {
				filtered, err := filterCNTeamBattleGroups(method[field], allowed)
				if err != nil {
					return nil, err
				}
				method[field], err = json.Marshal(filtered)
				if err != nil {
					return nil, err
				}
			}
		}
		return method, nil
	})
}

func filterCNTeamBattleGroups(content json.RawMessage, allowed map[int]struct{}) ([]json.RawMessage, error) {
	var groups []json.RawMessage
	if len(content) == 0 || bytes.Equal(content, []byte("null")) {
		return []json.RawMessage{}, nil
	}
	if err := json.Unmarshal(content, &groups); err != nil {
		return nil, err
	}
	filtered := make([]json.RawMessage, 0, len(groups))
	for _, group := range groups {
		var identity struct {
			GroupID int `json:"0"`
		}
		if err := json.Unmarshal(group, &identity); err != nil {
			return nil, err
		}
		if _, exists := allowed[identity.GroupID]; exists {
			filtered = append(filtered, group)
		}
	}
	return filtered, nil
}
