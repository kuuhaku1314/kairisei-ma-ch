package cnbootstrap

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"

	"kairisei.local/server/internal/accounthttp"
	"kairisei.local/server/internal/accountstore"
)

type bufferedResponseWriter struct {
	header http.Header
	status int
	body   bytes.Buffer
}

func newBufferedResponseWriter() *bufferedResponseWriter {
	return &bufferedResponseWriter{header: make(http.Header)}
}

func (writer *bufferedResponseWriter) Header() http.Header {
	return writer.header
}

func (writer *bufferedResponseWriter) WriteHeader(status int) {
	if writer.status == 0 {
		writer.status = status
	}
}

func (writer *bufferedResponseWriter) Write(content []byte) (int, error) {
	if writer.status == 0 {
		writer.status = http.StatusOK
	}
	return writer.body.Write(content)
}

func cnBootstrapCardShow2(businessHandler http.Handler) http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		payload, err := readCNSessionPayload(request, "CardShow2")
		if err != nil {
			http.Error(writer, err.Error(), http.StatusUnauthorized)
			return
		}
		deckInfoType := 0
		if len(payload) != 0 {
			var fields map[string]json.RawMessage
			if err := json.Unmarshal(payload, &fields); err != nil || fields == nil || len(fields) > 1 {
				http.Error(writer, "invalid CN CardShow2 payload", http.StatusBadRequest)
				return
			}
			if raw, exists := fields["0"]; exists {
				if err := json.Unmarshal(raw, &deckInfoType); err != nil || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) || deckInfoType < 0 {
					http.Error(writer, "invalid CN CardShow2 deck_info_type", http.StatusBadRequest)
					return
				}
			} else if len(fields) != 0 {
				http.Error(writer, "unexpected CN CardShow2 field", http.StatusBadRequest)
				return
			}
		}
		forward := []byte(`{"deck_info_type":` + strconv.Itoa(deckInfoType) + `}`)
		businessHandler.ServeHTTP(writer, adaptCNBusinessRequest(request, http.MethodPost, "/CardShow2", forward))
	}
}

func cnBootstrapCardContainerShow(businessHandler http.Handler) http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		if err := requireCNSessionOnly(request, "CardContainerShow"); err != nil {
			http.Error(writer, err.Error(), http.StatusUnauthorized)
			return
		}
		forwardCNBusiness(
			writer,
			adaptCNBusinessRequest(request, http.MethodPost, "/CardContainerShow", nil),
			businessHandler,
			adaptCNCardContainerShowResponse,
		)
	}
}

func cnBootstrapCardMove(businessHandler http.Handler) http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		payload, err := normalizeCNOptionalFields(
			request,
			"CardMove",
			[]string{"to_slot", "move_uniqids"},
			[]string{"to_slot", "move_uniqids"},
		)
		if err != nil {
			http.Error(writer, err.Error(), http.StatusBadRequest)
			return
		}
		forwardCNBusiness(
			writer,
			adaptCNBusinessRequest(request, http.MethodPost, "/CardMove", payload),
			businessHandler,
			adaptCNCardMoveResponse,
		)
	}
}

func cnBootstrapCardCollectionShow(businessHandler http.Handler) http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		if err := requireCNSessionOnly(request, "CardCollectionShow"); err != nil {
			http.Error(writer, err.Error(), http.StatusUnauthorized)
			return
		}
		forwardCNBusiness(
			writer,
			adaptCNBusinessRequest(request, http.MethodPost, "/CardCollectionShow", nil),
			businessHandler,
			adaptCNCardCollectionShowResponse,
		)
	}
}

func cnBootstrapCardCategoryGet(businessHandler http.Handler) http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		if err := requireCNSessionOnly(request, "CardCategoryGet"); err != nil {
			http.Error(writer, err.Error(), http.StatusUnauthorized)
			return
		}
		// The CN client sends this read-only operation as POST with only its
		// session prefix.
		businessHandler.ServeHTTP(
			writer,
			adaptCNBusinessRequest(request, http.MethodPost, "/CardCategoryGet", nil),
		)
	}
}

func cnBootstrapCardDeckSet(businessHandler http.Handler) http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		payload, err := readCNSessionPayload(request, "CardDeckSet")
		if err != nil {
			http.Error(writer, err.Error(), http.StatusUnauthorized)
			return
		}
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(payload, &fields); err != nil || len(fields) != 1 {
			http.Error(writer, "invalid CN CardDeckSet payload", http.StatusBadRequest)
			return
		}
		var decks []json.RawMessage
		if raw, ok := fields["decks"]; !ok || json.Unmarshal(raw, &decks) != nil {
			http.Error(writer, "invalid CN CardDeckSet decks", http.StatusBadRequest)
			return
		}
		forwardCNBusiness(
			writer,
			adaptCNBusinessRequest(request, http.MethodPost, "/CardDeckSet", payload),
			businessHandler,
			adaptCNCardDeckSetResponse,
		)
	}
}

func cnBootstrapCardFusion2(businessHandler http.Handler) http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		payload, err := normalizeCNOptionalFields(
			request,
			"CardFusion2",
			[]string{"base_uniqid", "add_uniqids", "add_container_uniqids", "add_cardids", "add_stackcards"},
			[]string{"base_uniqid"},
		)
		if err != nil {
			http.Error(writer, err.Error(), http.StatusBadRequest)
			return
		}
		forwardCNBusiness(
			writer,
			adaptCNBusinessRequest(request, http.MethodPost, "/CardFusion2", payload),
			businessHandler,
			adaptCNCardFusion2Response,
		)
	}
}

func cnBootstrapCardEvolution(businessHandler http.Handler) http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		payload, err := normalizeCNOptionalFields(
			request,
			"CardEvolution",
			[]string{"base_uniqid", "to_cardid", "add_uniqids", "add_container_uniqids", "add_cardids", "add_itemids"},
			[]string{"base_uniqid", "to_cardid"},
		)
		if err != nil {
			http.Error(writer, err.Error(), http.StatusBadRequest)
			return
		}
		forwardCNBusiness(
			writer,
			adaptCNBusinessRequest(request, http.MethodPost, "/CardEvolution", payload),
			businessHandler,
			adaptCNCardEvolutionResponse,
		)
	}
}

func cnBootstrapCardLoveUp(businessHandler http.Handler) http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		payload, err := normalizeCNOptionalFields(
			request,
			"CardLoveUp",
			[]string{"base_uniqid", "use_items"},
			[]string{"base_uniqid", "use_items"},
		)
		if err != nil {
			http.Error(writer, err.Error(), http.StatusBadRequest)
			return
		}
		forwardCNBusiness(
			writer,
			adaptCNBusinessRequest(request, http.MethodPost, "/CardLoveUp", payload),
			businessHandler,
			adaptCNCardLoveUpResponse,
		)
	}
}

func cnBootstrapCardDecompose(businessHandler http.Handler) http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		payload, err := readCNSessionPayload(request, "CardDecompose")
		if err != nil {
			http.Error(writer, err.Error(), http.StatusUnauthorized)
			return
		}
		if err := requireCNExactFields(payload, "CardDecompose", "base_uniqid", "type"); err != nil {
			http.Error(writer, err.Error(), http.StatusBadRequest)
			return
		}
		forwardCNBusiness(
			writer,
			adaptCNBusinessRequest(request, http.MethodPost, "/CardDecompose", payload),
			businessHandler,
			adaptCNCardDecomposeResponse,
		)
	}
}

func cnBootstrapCardSell(businessHandler http.Handler) http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		payload, err := normalizeCNOptionalFields(
			request,
			"CardSell",
			[]string{"uniqids", "cardids"},
			nil,
		)
		if err != nil {
			http.Error(writer, err.Error(), http.StatusBadRequest)
			return
		}
		forwardCNBusiness(
			writer,
			adaptCNBusinessRequest(request, http.MethodPost, "/CardSell", payload),
			businessHandler,
			adaptCNCardSellResponse,
		)
	}
}

func cnBootstrapCardContainerSell(businessHandler http.Handler) http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		payload, err := readCNSessionPayload(request, "CardContainerSell")
		if err != nil {
			http.Error(writer, err.Error(), http.StatusUnauthorized)
			return
		}
		if err := requireCNExactFields(payload, "CardContainerSell", "uniqids"); err != nil {
			http.Error(writer, err.Error(), http.StatusBadRequest)
			return
		}
		forwardCNBusiness(
			writer,
			adaptCNBusinessRequest(request, http.MethodPost, "/CardContainerSell", payload),
			businessHandler,
			adaptCNCardContainerSellResponse,
		)
	}
}

func normalizeCNOptionalFields(
	request *http.Request,
	operation string,
	expected []string,
	required []string,
) ([]byte, error) {
	payload, err := readCNSessionPayload(request, operation)
	if err != nil {
		return nil, err
	}
	// No-field DTOs (e.g. StoryTeamBattleEnd) are sent as the session alone.
	// Authentication is already checked above; only normalize the empty DTO.
	if len(payload) == 0 && len(expected) == 0 && len(required) == 0 {
		return []byte("{}"), nil
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(payload, &fields); err != nil {
		return nil, errors.New("invalid CN " + operation + " payload")
	}
	allowed := make(map[string]struct{}, len(expected))
	for _, name := range expected {
		allowed[name] = struct{}{}
	}
	for name := range fields {
		if _, exists := allowed[name]; !exists {
			return nil, errors.New("unexpected CN " + operation + " field: " + name)
		}
	}
	for _, name := range required {
		if _, exists := fields[name]; !exists {
			return nil, errors.New("missing CN " + operation + " field: " + name)
		}
	}
	for _, name := range expected {
		if _, exists := fields[name]; !exists {
			fields[name] = json.RawMessage(`[]`)
		}
	}
	return json.Marshal(fields)
}

func cnBootstrapSphereShow(businessHandler http.Handler) http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		if err := requireCNSessionOnly(request, "SphrShow"); err != nil {
			http.Error(writer, err.Error(), http.StatusUnauthorized)
			return
		}
		forwardCNBusiness(
			writer,
			adaptCNBusinessRequest(request, http.MethodPost, "/SphrShow", nil),
			businessHandler,
			func(content []byte) ([]byte, error) {
				return adaptCNProtocolMethod(content, func(method map[string]json.RawMessage) (any, error) {
					spheres, err := remapJSONArray(method["0"], cnSphereInfoNamedFieldMap)
					if err != nil {
						return nil, err
					}
					return map[string]any{"sphrs": spheres}, nil
				})
			},
		)
	}
}

func cnBootstrapSphereFusion2(businessHandler http.Handler) http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		payload, err := normalizeCNOptionalFields(
			request, "SphrFusion2",
			[]string{"base_uniqid", "add_inputs"},
			[]string{"base_uniqid", "add_inputs"},
		)
		if err != nil {
			http.Error(writer, err.Error(), http.StatusBadRequest)
			return
		}
		forwardCNBusiness(
			writer,
			adaptCNBusinessRequest(request, http.MethodPost, "/SphrFusion2", payload),
			businessHandler,
			adaptCNSphereFusion2Response,
		)
	}
}

func cnBootstrapSphereEvolution(businessHandler http.Handler) http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		payload, err := normalizeCNOptionalFields(
			request, "SphrEvolution",
			[]string{"base_uniqid", "add_uniqid", "add_cardid"},
			[]string{"base_uniqid", "add_uniqid", "add_cardid"},
		)
		if err != nil {
			http.Error(writer, err.Error(), http.StatusBadRequest)
			return
		}
		forwardCNBusiness(
			writer,
			adaptCNBusinessRequest(request, http.MethodPost, "/SphrEvolution", payload),
			businessHandler,
			adaptCNSphereEvolutionResponse,
		)
	}
}

func cnBootstrapSphereSell(businessHandler http.Handler) http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		payload, err := normalizeCNOptionalFields(
			request, "SphrSell", []string{"uniqids"}, []string{"uniqids"},
		)
		if err != nil {
			http.Error(writer, err.Error(), http.StatusBadRequest)
			return
		}
		forwardCNBusiness(
			writer,
			adaptCNBusinessRequest(request, http.MethodPost, "/SphrSell", payload),
			businessHandler,
			adaptCNSphereSellResponse,
		)
	}
}

func cnBootstrapSphereLock(businessHandler http.Handler, unlock bool) http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		operation, route := "SphrLock", "/SphrLock"
		if unlock {
			operation, route = "SphrUnlock", "/SphrUnlock"
		}
		payload, err := normalizeCNOptionalFields(
			request, operation, []string{"uniqid"}, []string{"uniqid"},
		)
		if err != nil {
			http.Error(writer, err.Error(), http.StatusBadRequest)
			return
		}
		forwardCNBusiness(
			writer,
			adaptCNBusinessRequest(request, http.MethodPost, route, payload),
			businessHandler,
			func(content []byte) ([]byte, error) {
				return adaptCNProtocolMethod(content, func(map[string]json.RawMessage) (any, error) {
					return struct{}{}, nil
				})
			},
		)
	}
}

func cnBootstrapBuddyShow(businessHandler http.Handler) http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		if err := requireCNSessionOnly(request, "BuddyShow"); err != nil {
			http.Error(writer, err.Error(), http.StatusUnauthorized)
			return
		}
		forwardCNBusiness(
			writer,
			adaptCNBusinessRequest(request, http.MethodPost, "/BuddyShow", nil),
			businessHandler,
			func(content []byte) ([]byte, error) {
				return adaptCNProtocolMethod(content, func(method map[string]json.RawMessage) (any, error) {
					buddies, err := remapJSONArray(method["0"], cnBuddyInfoNamedFieldMap)
					if err != nil {
						return nil, err
					}
					return map[string]any{"buddys": buddies}, nil
				})
			},
		)
	}
}

func cnBootstrapBuddyFusion(businessHandler http.Handler) http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		payload, err := normalizeCNOptionalFields(
			request, "BuddyFusion",
			[]string{"base_uniqid", "add_inputs"},
			[]string{"base_uniqid", "add_inputs"},
		)
		if err != nil {
			http.Error(writer, err.Error(), http.StatusBadRequest)
			return
		}
		forwardCNBusiness(
			writer,
			adaptCNBusinessRequest(request, http.MethodPost, "/BuddyFusion", payload),
			businessHandler,
			adaptCNBuddyFusionResponse,
		)
	}
}

func cnBootstrapBuddyEvolution(businessHandler http.Handler) http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		payload, err := normalizeCNOptionalFields(
			request, "BuddyEvolution",
			[]string{"base_uniqid", "add_buddy_uniqid", "add_cardid"},
			[]string{"base_uniqid", "add_buddy_uniqid", "add_cardid"},
		)
		if err != nil {
			http.Error(writer, err.Error(), http.StatusBadRequest)
			return
		}
		forwardCNBusiness(
			writer,
			adaptCNBusinessRequest(request, http.MethodPost, "/BuddyEvolution", payload),
			businessHandler,
			adaptCNBuddyEvolutionResponse,
		)
	}
}

func cnBootstrapBuddySell(businessHandler http.Handler) http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		payload, err := normalizeCNOptionalFields(
			request, "BuddySell", []string{"uniqids"}, []string{"uniqids"},
		)
		if err != nil {
			http.Error(writer, err.Error(), http.StatusBadRequest)
			return
		}
		forwardCNBusiness(
			writer,
			adaptCNBusinessRequest(request, http.MethodPost, "/BuddySell", payload),
			businessHandler,
			adaptCNBuddySellResponse,
		)
	}
}

func cnBootstrapBuddyLock(businessHandler http.Handler, unlock bool) http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		operation, route := "BuddyLock", "/BuddyLock"
		if unlock {
			operation, route = "BuddyUnlock", "/BuddyUnlock"
		}
		payload, err := normalizeCNOptionalFields(
			request, operation, []string{"uniqid"}, []string{"uniqid"},
		)
		if err != nil {
			http.Error(writer, err.Error(), http.StatusBadRequest)
			return
		}
		forwardCNBusiness(
			writer,
			adaptCNBusinessRequest(request, http.MethodPost, route, payload),
			businessHandler,
			func(content []byte) ([]byte, error) {
				return adaptCNProtocolMethod(content, func(map[string]json.RawMessage) (any, error) {
					return struct{}{}, nil
				})
			},
		)
	}
}

func requireCNSessionOnly(request *http.Request, operation string) error {
	payload, err := readCNSessionPayload(request, operation)
	if err != nil {
		return err
	}
	if len(payload) != 0 {
		return errors.New("invalid CN " + operation + " payload")
	}
	return nil
}

func readCNSessionPayload(request *http.Request, operation string) ([]byte, error) {
	body, err := io.ReadAll(io.LimitReader(request.Body, maxCapturedBody+1))
	if err != nil || len(body) > maxCapturedBody {
		return nil, errors.New("read CN " + operation + " request")
	}
	_, payload, ok := splitCNSessionPayload(body)
	if !ok {
		return nil, errors.New("invalid CN " + operation + " session")
	}
	userID, ok := request.Context().Value(cnAuthenticatedUserKey{}).(int)
	if !ok || userID < accountstore.PrimaryUserID {
		return nil, errors.New("unauthenticated CN " + operation + " session")
	}
	request.Header.Set(accounthttp.AccountUserHeader, strconv.Itoa(userID))
	return payload, nil
}

func splitCNSessionPayload(body []byte) (string, []byte, bool) {
	if len(body) == 0 {
		return "", nil, false
	}
	payloadIndex := bytes.IndexAny(body, "{[")
	if payloadIndex == 0 {
		return "", nil, false
	}
	if payloadIndex < 0 {
		payloadIndex = len(body)
	}
	session := body[:payloadIndex]
	if len(session) < 16 || len(session) > 128 {
		return "", nil, false
	}
	for _, character := range session {
		if (character < 'a' || character > 'z') &&
			(character < 'A' || character > 'Z') &&
			(character < '0' || character > '9') &&
			character != '-' && character != '_' {
			return "", nil, false
		}
	}
	return string(session), body[payloadIndex:], true
}

func adaptCNBusinessRequest(request *http.Request, method string, requestPath string, body []byte) *http.Request {
	adapted := request.Clone(request.Context())
	adaptedURL := *request.URL
	adaptedURL.Path = requestPath
	adapted.URL = &adaptedURL
	adapted.Method = method
	adapted.Body = io.NopCloser(bytes.NewReader(body))
	adapted.ContentLength = int64(len(body))
	return adapted
}

func forwardCNBusiness(
	writer http.ResponseWriter,
	request *http.Request,
	businessHandler http.Handler,
	adapt func([]byte) ([]byte, error),
) {
	buffered := newBufferedResponseWriter()
	businessHandler.ServeHTTP(buffered, request)
	status := buffered.status
	if status == 0 {
		status = http.StatusOK
	}
	content := buffered.body.Bytes()
	if status >= 200 && status < 300 {
		adapted, err := adapt(content)
		if err != nil {
			http.Error(writer, "adapt CN business response", http.StatusInternalServerError)
			return
		}
		content = adapted
	}
	for key, values := range buffered.header {
		writer.Header()[key] = append([]string(nil), values...)
	}
	writer.Header().Set("Content-Length", strconv.Itoa(len(content)))
	writer.WriteHeader(status)
	_, _ = writer.Write(content)
}

func adaptCNCardContainerShowResponse(content []byte) ([]byte, error) {
	return adaptCNProtocolMethod(content, func(method map[string]json.RawMessage) (any, error) {
		cards, err := remapJSONArray(method["0"], cnCardInfoFieldMap)
		if err != nil {
			return nil, err
		}
		return map[string]any{"0": cards}, nil
	})
}

func adaptCNCardMoveResponse(content []byte) ([]byte, error) {
	return adaptCNProtocolMethod(content, func(method map[string]json.RawMessage) (any, error) {
		cards, err := remapJSONArray(method["0"], cnCardInfoNamedFieldMap)
		if err != nil {
			return nil, err
		}
		decks, err := remapJSONArray(method["1"], cnDeckInfoNamedFieldMap)
		if err != nil {
			return nil, err
		}
		return map[string]any{"upd_cards": cards, "upd_decks": decks}, nil
	})
}

func adaptCNCardCollectionShowResponse(content []byte) ([]byte, error) {
	return adaptCNProtocolMethod(content, func(method map[string]json.RawMessage) (any, error) {
		var pages []struct {
			Cards []struct {
				CardID   int `json:"cardid"`
				StateBit int `json:"state_bit"`
			} `json:"cards"`
		}
		if raw := method["pages"]; len(raw) != 0 && !bytes.Equal(raw, []byte("null")) {
			if err := json.Unmarshal(raw, &pages); err != nil {
				return nil, err
			}
		}
		mappedPages := make([]map[string]any, len(pages))
		for pageIndex, page := range pages {
			cards := make([]map[string]int, len(page.Cards))
			for cardIndex, card := range page.Cards {
				cards[cardIndex] = map[string]int{
					"0": card.CardID,
					"1": card.StateBit,
				}
			}
			mappedPages[pageIndex] = map[string]any{"0": cards}
		}
		return map[string]any{
			"0": mappedPages,
			"1": rawOrZero(method["find_card_num"]),
			"2": rawOrZero(method["find_card_max"]),
		}, nil
	})
}

func adaptCNCardDeckSetResponse(content []byte) ([]byte, error) {
	return adaptCNProtocolMethod(content, func(method map[string]json.RawMessage) (any, error) {
		cards, err := remapJSONArray(method["0"], cnCardInfoNamedFieldMap)
		if err != nil {
			return nil, err
		}
		decks, err := remapJSONArray(method["1"], cnDeckInfoNamedFieldMap)
		if err != nil {
			return nil, err
		}
		return map[string]any{"cards": cards, "decks": decks}, nil
	})
}

func adaptCNCardFusion2Response(content []byte) ([]byte, error) {
	return adaptCNProtocolMethod(content, func(method map[string]json.RawMessage) (any, error) {
		var result map[string]json.RawMessage
		if err := json.Unmarshal(method["result_card"], &result); err != nil {
			return nil, err
		}
		card, err := remapJSONObject(result["card"], cnCardInfoNamedFieldMap)
		if err != nil {
			return nil, err
		}
		oldCard, err := remapJSONObject(result["old_card"], cnCardInfoNamedFieldMap)
		if err != nil {
			return nil, err
		}
		decks, err := remapJSONArray(method["decks"], cnDeckInfoNamedFieldMap)
		if err != nil {
			return nil, err
		}
		return map[string]any{
			"result_card": map[string]any{
				"card":         card,
				"old_card":     oldCard,
				"success_type": rawOrZero(result["success_type"]),
				"exp_up":       rawOrEmptyArray(result["exp_up"]),
				"fame_up":      rawOrEmptyArray(result["fame_up"]),
				"rewards":      rawOrEmptyArray(result["rewards"]),
			},
			"gold":                   rawOrZero(method["gold"]),
			"card_num":               rawOrZero(method["card_num"]),
			"decks":                  decks,
			"back_uniqids":           rawOrEmptyArray(method["back_uniqids"]),
			"back_container_uniqids": rawOrEmptyArray(method["back_container_uniqids"]),
			"back_stack_cards":       rawOrEmptyArray(method["back_stack_cards"]),
		}, nil
	})
}

func adaptCNCardEvolutionResponse(content []byte) ([]byte, error) {
	return adaptCNProtocolMethod(content, func(method map[string]json.RawMessage) (any, error) {
		baseCard, err := remapJSONObject(method["base_card"], cnCardInfoNamedFieldMap)
		if err != nil {
			return nil, err
		}
		decks, err := remapJSONArray(method["decks"], cnDeckInfoNamedFieldMap)
		if err != nil {
			return nil, err
		}
		return map[string]any{
			"base_card": baseCard,
			"gold":      rawOrZero(method["gold"]),
			"card_num":  rawOrZero(method["card_num"]),
			"decks":     decks,
			"reward":    cnEmptyResultReward(),
		}, nil
	})
}

func adaptCNCardDecomposeResponse(content []byte) ([]byte, error) {
	return adaptCNProtocolMethod(content, func(method map[string]json.RawMessage) (any, error) {
		decks, err := remapJSONArray(method["decks"], cnDeckInfoNamedFieldMap)
		if err != nil {
			return nil, err
		}
		return map[string]any{
			"stive":          rawOrZero(method["stive"]),
			"decompose_type": rawOrZero(method["decompose_type"]),
			"uniqid":         rawOrZero(method["uniqid"]),
			"decks":          decks,
		}, nil
	})
}

func adaptCNCardLoveUpResponse(content []byte) ([]byte, error) {
	return adaptCNProtocolMethod(content, func(method map[string]json.RawMessage) (any, error) {
		baseCard, err := remapJSONObject(method["base_card"], cnCardInfoNamedFieldMap)
		if err != nil {
			return nil, err
		}
		var result map[string]json.RawMessage
		if err := json.Unmarshal(method["result_card"], &result); err != nil {
			return nil, err
		}
		resultCard, err := remapJSONObject(result["card"], cnCardInfoNamedFieldMap)
		if err != nil {
			return nil, err
		}
		return map[string]any{
			"base_card": baseCard,
			"item":      rawOrEmptyArray(method["item"]),
			"result_card": map[string]any{
				"card":    resultCard,
				"love_up": rawOrEmptyArray(result["love_up"]),
				"rewards": rawOrEmptyArray(result["rewards"]),
			},
			"gold": rawOrZero(method["gold"]),
		}, nil
	})
}

func cnEmptyResultReward() map[string]any {
	return map[string]any{
		"reward": map[string]any{
			"type":          0,
			"num":           0,
			"reward_typeid": 0,
			"card_lv":       0,
			"card_fame":     0,
			"card_love":     0,
			"card_skill_lv": []any{},
		},
		"uniqid":           []any{},
		"is_new":           0,
		"auto_fusion_used": 0,
		"auto_loveup_used": 0,
		"add":              []any{},
	}
}

func adaptCNCardSellResponse(content []byte) ([]byte, error) {
	return adaptCNProtocolMethod(content, func(method map[string]json.RawMessage) (any, error) {
		decks, err := remapJSONArray(method["decks"], cnDeckInfoNamedFieldMap)
		if err != nil {
			return nil, err
		}
		return map[string]any{
			"card_num": rawOrZero(method["card_num"]),
			"get_gold": rawOrZero(method["get_gold"]),
			"gold":     rawOrZero(method["gold"]),
			"decks":    decks,
		}, nil
	})
}

func adaptCNCardContainerSellResponse(content []byte) ([]byte, error) {
	return adaptCNProtocolMethod(content, func(method map[string]json.RawMessage) (any, error) {
		return map[string]any{
			"card_container_num": rawOrZero(method["card_container_num"]),
			"get_gold":           rawOrZero(method["get_gold"]),
			"gold":               rawOrZero(method["gold"]),
		}, nil
	})
}

func adaptCNSphereFusion2Response(content []byte) ([]byte, error) {
	return adaptCNProtocolMethod(content, func(method map[string]json.RawMessage) (any, error) {
		baseSphere, err := remapJSONObject(method["1"], cnSphereInfoNamedFieldMap)
		if err != nil {
			return nil, err
		}
		decks, err := remapJSONArray(method["4"], cnDeckInfoNamedFieldMap)
		if err != nil {
			return nil, err
		}
		return map[string]any{
			"success_type":     rawOrZero(method["0"]),
			"base_sphr":        baseSphere,
			"gold":             rawOrZero(method["2"]),
			"sphr_num":         rawOrZero(method["3"]),
			"decks":            decks,
			"back_uniqids":     rawOrEmptyArray(method["5"]),
			"back_stack_cards": rawOrEmptyArray(method["6"]),
		}, nil
	})
}

func adaptCNSphereEvolutionResponse(content []byte) ([]byte, error) {
	return adaptCNProtocolMethod(content, func(method map[string]json.RawMessage) (any, error) {
		baseSphere, err := remapJSONObject(method["0"], cnSphereInfoNamedFieldMap)
		if err != nil {
			return nil, err
		}
		decks, err := remapJSONArray(method["3"], cnDeckInfoNamedFieldMap)
		if err != nil {
			return nil, err
		}
		return map[string]any{
			"base_sphr": baseSphere,
			"gold":      rawOrZero(method["1"]),
			"sphr_num":  rawOrZero(method["2"]),
			"decks":     decks,
			"reward":    cnEmptyResultReward(),
		}, nil
	})
}

func adaptCNSphereSellResponse(content []byte) ([]byte, error) {
	return adaptCNProtocolMethod(content, func(method map[string]json.RawMessage) (any, error) {
		decks, err := remapJSONArray(method["3"], cnDeckInfoNamedFieldMap)
		if err != nil {
			return nil, err
		}
		return map[string]any{
			"sphr_num": rawOrZero(method["0"]),
			"get_gold": rawOrZero(method["1"]),
			"gold":     rawOrZero(method["2"]),
			"decks":    decks,
		}, nil
	})
}

func adaptCNBuddyFusionResponse(content []byte) ([]byte, error) {
	return adaptCNProtocolMethod(content, func(method map[string]json.RawMessage) (any, error) {
		baseBuddy, err := remapJSONObject(method["1"], cnBuddyInfoNamedFieldMap)
		if err != nil {
			return nil, err
		}
		decks, err := remapJSONArray(method["4"], cnDeckInfoNamedFieldMap)
		if err != nil {
			return nil, err
		}
		backStackCards, err := remapJSONArray(method["6"], cnCardStackInfoNamedFieldMap)
		if err != nil {
			return nil, err
		}
		return map[string]any{
			"success_type":       rawOrZero(method["0"]),
			"base_buddy":         baseBuddy,
			"gold":               rawOrZero(method["2"]),
			"buddy_num":          rawOrZero(method["3"]),
			"decks":              decks,
			"back_buddy_uniqids": rawOrEmptyArray(method["5"]),
			"back_stack_cards":   backStackCards,
		}, nil
	})
}

func adaptCNBuddyEvolutionResponse(content []byte) ([]byte, error) {
	return adaptCNProtocolMethod(content, func(method map[string]json.RawMessage) (any, error) {
		baseBuddy, err := remapJSONObject(method["0"], cnBuddyInfoNamedFieldMap)
		if err != nil {
			return nil, err
		}
		decks, err := remapJSONArray(method["3"], cnDeckInfoNamedFieldMap)
		if err != nil {
			return nil, err
		}
		reward := any(cnEmptyResultReward())
		if raw := method["4"]; len(raw) != 0 && !bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			reward = raw
		}
		return map[string]any{
			"base_buddy": baseBuddy,
			"gold":       rawOrZero(method["1"]),
			"buddy_num":  rawOrZero(method["2"]),
			"decks":      decks,
			"reward":     reward,
		}, nil
	})
}

func adaptCNBuddySellResponse(content []byte) ([]byte, error) {
	return adaptCNProtocolMethod(content, func(method map[string]json.RawMessage) (any, error) {
		decks, err := remapJSONArray(method["3"], cnDeckInfoNamedFieldMap)
		if err != nil {
			return nil, err
		}
		return map[string]any{
			"buddy_num": rawOrZero(method["0"]),
			"get_gold":  rawOrZero(method["1"]),
			"gold":      rawOrZero(method["2"]),
			"decks":     decks,
		}, nil
	})
}

func adaptCNProtocolMethod(
	content []byte,
	adapt func(map[string]json.RawMessage) (any, error),
) ([]byte, error) {
	segments := bytes.Split(content, []byte{'\n'})
	if len(segments) != 3 {
		return nil, errors.New("unexpected protocol segment count")
	}
	var common struct {
		ResultCode int `json:"res_code"`
	}
	if err := json.Unmarshal(segments[0], &common); err != nil {
		return nil, err
	}
	// The original generated receiver skips method fields on a common error.
	// Do not turn an expected business rejection into an adapter/HTTP failure.
	if common.ResultCode < 0 {
		return content, nil
	}
	var method map[string]json.RawMessage
	if err := json.Unmarshal(segments[1], &method); err != nil {
		return nil, err
	}
	adapted, err := adapt(method)
	if err != nil {
		return nil, err
	}
	methodJSON, err := json.Marshal(adapted)
	if err != nil {
		return nil, err
	}
	return bytes.Join([][]byte{segments[0], methodJSON, segments[2]}, []byte{'\n'}), nil
}

func remapJSONArray(raw json.RawMessage, fields map[string]string) ([]map[string]json.RawMessage, error) {
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return []map[string]json.RawMessage{}, nil
	}
	var entries []map[string]json.RawMessage
	if err := json.Unmarshal(raw, &entries); err != nil {
		return nil, err
	}
	result := make([]map[string]json.RawMessage, len(entries))
	for index, entry := range entries {
		mapped := make(map[string]json.RawMessage, len(fields))
		for target, source := range fields {
			if value, ok := entry[source]; ok {
				mapped[target] = value
			}
		}
		result[index] = mapped
	}
	return result, nil
}

func remapJSONObject(raw json.RawMessage, fields map[string]string) (map[string]json.RawMessage, error) {
	var entry map[string]json.RawMessage
	if err := json.Unmarshal(raw, &entry); err != nil {
		return nil, err
	}
	mapped := make(map[string]json.RawMessage, len(fields))
	for target, source := range fields {
		if value, ok := entry[source]; ok {
			mapped[target] = value
		}
	}
	return mapped, nil
}

func rawOrEmptyArray(raw json.RawMessage) any {
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return []any{}
	}
	return raw
}

func rawOrZero(raw json.RawMessage) any {
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return 0
	}
	return raw
}

var cnCardInfoFieldMap = map[string]string{
	"0": "0", "1": "1", "2": "2", "3": "3", "4": "4",
	"5": "5", "6": "6", "7": "7", "8": "8", "9": "9",
	"10": "10", "11": "13", "12": "14", "13": "15", "14": "16",
	"15": "17", "16": "18", "17": "19", "18": "20", "19": "21",
	"alchemist_param": "11", "summons_param": "12",
}

var cnCardInfoNamedFieldMap = map[string]string{
	"uniqid": "0", "cardid": "1", "lv": "2", "lv_max": "3", "exp": "4",
	"love": "5", "skill_lv": "6", "hp": "7", "atkp": "8", "intp": "9",
	"mndp": "10", "next_lv_exp": "13", "now_lv_exp": "14", "add_exp": "15",
	"base_add_price": "16", "is_lock": "17", "create_time": "18", "fame": "19",
	"slot": "20", "passive": "21",
	"alchemist_param": "11", "summons_param": "12",
}

var cnDeckInfoNamedFieldMap = map[string]string{
	"arthur_type": "0", "idx": "1", "job_type": "2", "leader_card_idx": "3",
	"card_uniqid": "4", "support_card_uniqid": "5", "sphr_uniqid": "6",
	"buddy_uniqid": "7", "name": "8", "is_active": "9", "is_rental": "10",
	"deck_rank": "11",
}

var cnBuddyInfoNamedFieldMap = map[string]string{
	"uniqid": "0", "buddyid": "1", "lv": "2", "exp": "3",
	"next_lv_exp": "4", "now_lv_exp": "5", "add_exp": "6",
	"base_add_price": "7", "is_lock": "8", "create_time": "9",
}

var cnCardStackInfoNamedFieldMap = map[string]string{
	"cardid": "0", "num": "1", "hp": "2", "atkp": "3", "intp": "4",
	"mndp": "5", "add_exp": "6", "base_add_price": "7",
}

var cnSphereInfoNamedFieldMap = map[string]string{
	"uniqid": "0", "sphrid": "1", "lv": "2", "exp": "3",
	"next_lv_exp": "4", "now_lv_exp": "5", "add_exp": "6",
	"base_add_price": "7", "is_lock": "8", "create_time": "9",
}
