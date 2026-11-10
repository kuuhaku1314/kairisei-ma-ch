package cnbootstrap

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"kairisei.local/server/internal/protocol"
	"net/http"
	"strings"

	"kairisei.local/server/internal/accountstore"
)

func cnBootstrapCommon() map[string]any {
	return map[string]any{
		"res_code":            0,
		"res_str":             "",
		"notification":        []any{},
		"revision":            protocol.CatalogRevision,
		"is_appupdate":        0,
		"res_err_action":      0,
		"res_is_del_savedata": 0,
	}
}

func cnBootstrapPing(writer http.ResponseWriter, _ *http.Request) {
	writeCNProtocolResponse(writer, cnBootstrapCommon(), map[string]any{
		"stamp": 0,
	})
}

func cnBootstrapConnect(businessHandler http.Handler) http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		payload, err := readCNSessionPayload(request, "Connect")
		if err != nil {
			http.Error(writer, err.Error(), http.StatusUnauthorized)
			return
		}
		if len(payload) != 0 {
			var fields map[string]json.RawMessage
			if json.Unmarshal(payload, &fields) != nil || len(fields) != 0 {
				http.Error(writer, "invalid CN Connect payload", http.StatusBadRequest)
				return
			}
		}
		forwardCNBusiness(
			writer,
			adaptCNBusinessRequest(
				request,
				http.MethodPost,
				"/__domain/Connect",
				[]byte(cn602LocalSession),
			),
			businessHandler,
			adaptCNConnectResponse,
		)
	}
}

func adaptCNConnectResponse(content []byte) ([]byte, error) {
	segments := bytes.Split(content, []byte{'\n'})
	if len(segments) != 3 {
		return nil, errors.New("unexpected CN Connect protocol segment count")
	}
	return bytes.Join(segments[:2], []byte{'\n'}), nil
}

func cnBootstrapClickLog(writer http.ResponseWriter, _ *http.Request) {
	// Statistics are outside the local server's scope. The CN 6.0.2 client
	// nevertheless requires the ordinary three-segment protocol envelope before
	// it can continue its business state machine, so acknowledge without parsing
	// or persisting the reported click data.
	writeCNProtocolResponseWithPopup(writer, cnBootstrapCommon(), map[string]any{})
}

func cnBootstrapTutorialProgress(writer http.ResponseWriter, request *http.Request) {
	payload, err := readCNSessionPayload(request, "TutorialProgress")
	if err != nil {
		http.Error(writer, err.Error(), http.StatusUnauthorized)
		return
	}
	if err := requireCNExactFields(payload, "TutorialProgress", "stepid", "model"); err != nil {
		http.Error(writer, err.Error(), http.StatusBadRequest)
		return
	}
	var progress struct {
		StepID int8   `json:"stepid"`
		Model  string `json:"model"`
	}
	if err := json.Unmarshal(payload, &progress); err != nil {
		http.Error(writer, "invalid CN TutorialProgress payload", http.StatusBadRequest)
		return
	}
	// The hash-locked CN client uses this request only as a KPI transport and
	// consumes no response fields. A transport failure nevertheless blocks the
	// first-install state machine, so acknowledge the exact DTO without storing
	// or interpreting the statistic. TutorialFlag remains the only persisted
	// tutorial-progress owner.
	writeCNProtocolResponseWithPopup(writer, cnBootstrapCommon(), map[string]any{})
}

func cnBootstrapClickNewVersionInfo(writer http.ResponseWriter, request *http.Request) {
	if err := requireCNSessionOnly(request, "ClickNewVersionInfo"); err != nil {
		http.Error(writer, err.Error(), http.StatusUnauthorized)
		return
	}
	writeCNProtocolResponseWithPopup(writer, cnBootstrapCommon(), map[string]any{
		"code": 0,
	})
}

func cnBootstrapTutorialFlag(businessHandler http.Handler) http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		payload, err := readCNSessionPayload(request, "TutorialFlag")
		if err != nil {
			http.Error(writer, err.Error(), http.StatusUnauthorized)
			return
		}
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(payload, &fields); err != nil || len(fields) != 2 {
			http.Error(writer, "invalid CN TutorialFlag payload", http.StatusBadRequest)
			return
		}
		flagRaw, hasFlag := fields["flag"]
		stepRaw, hasStep := fields["step"]
		var flag int64
		var step int
		if !hasFlag || !hasStep || json.Unmarshal(flagRaw, &flag) != nil || json.Unmarshal(stepRaw, &step) != nil {
			http.Error(writer, "invalid CN TutorialFlag fields", http.StatusBadRequest)
			return
		}
		domainBody, err := json.Marshal(map[string]int64{"flag": flag})
		if err != nil {
			http.Error(writer, "encode TutorialFlag adapter", http.StatusInternalServerError)
			return
		}
		adapted := request.Clone(request.Context())
		adaptedURL := *request.URL
		adaptedURL.Path = "/SetTutorialFlag"
		adapted.URL = &adaptedURL
		adapted.Body = io.NopCloser(bytes.NewReader(domainBody))
		adapted.ContentLength = int64(len(domainBody))
		businessHandler.ServeHTTP(writer, adapted)
	}
}

func cnBootstrapPushRegistration(writer http.ResponseWriter, _ *http.Request) {
	writer.Header().Set("Content-Type", "application/json; charset=utf-8")
	writer.Header().Set("Cache-Control", "no-store")
	_, _ = writer.Write([]byte("{}"))
}

func (app *application) moduleSwitches(writer http.ResponseWriter, _ *http.Request) {
	writer.Header().Set("Content-Type", "application/json; charset=utf-8")
	writer.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(writer).Encode(map[string]any{
		"res_code":   0,
		"mods_state": app.operations.ModuleSwitches(),
	})
}

type localLoginPayload struct {
	UUID             string                     `json:"uuid"`
	ClientVersion    string                     `json:"clver"`
	ResourcePlatform string                     `json:"resource_platform"`
	UnityVersion     string                     `json:"unity_version"`
	CreationProof    accountstore.CreationProof `json:"creation_proof"`
}

// Resource selection precedes the shared account/session business operation.
func localClientLogin(accounts *accountstore.Accounts, baseURL, cpkURL, imageURL string, selectResources func(localLoginPayload) (patchURL, versionURL string, err error)) http.HandlerFunc {
	// Bound UUID-login processing and waiters too; the password gateway has its
	// own separate limit. Do not queue unlimited retries behind the DB writer.
	loginSlots := make(chan struct{}, 8)
	return func(writer http.ResponseWriter, request *http.Request) {
		if request.Context().Err() != nil {
			return
		}
		select {
		case loginSlots <- struct{}{}:
			defer func() { <-loginSlots }()
		default:
			http.Error(writer, "CN local login is busy; retry later", http.StatusServiceUnavailable)
			return
		}
		body, err := io.ReadAll(io.LimitReader(request.Body, maxCapturedBody+1))
		if err != nil || len(body) == 0 || len(body) > maxCapturedBody {
			http.Error(writer, "read CN local login", http.StatusBadRequest)
			return
		}
		var payload localLoginPayload
		if err := json.Unmarshal(body, &payload); err != nil {
			http.Error(writer, "decode CN local login", http.StatusBadRequest)
			return
		}
		if payload.UUID == "" {
			http.Error(writer, "CN local login UUID is required", http.StatusBadRequest)
			return
		}
		if !cnClientVersionAllowed(payload.ClientVersion) {
			cnRejectOldClient(writer)
			return
		}
		patchURL, versionURL, err := selectResources(payload)
		if err != nil {
			http.Error(writer, err.Error(), http.StatusBadRequest)
			return
		}
		source, err := accountSource(request)
		if err != nil {
			http.Error(writer, "CN local login source is invalid", http.StatusBadRequest)
			return
		}
		identity, err := accounts.ResolveClientLogin(request.Context(), payload.UUID, source, payload.CreationProof)
		if err != nil {
			if request.Context().Err() != nil {
				return
			}
			if writeAccountAccessRejection(writer, err, true) || writeCreationRejection(writer, err, true) {
				return
			}
			http.Error(writer, "resolve CN local account", http.StatusBadRequest)
			return
		}
		writer.Header().Set("Content-Type", "application/json; charset=utf-8")
		writer.Header().Set("Cache-Control", "no-store")
		uid := fmt.Sprintf("local-cn-user-%d", identity.UserID)
		session := identity.SessionKey
		if identity.UserID == accountstore.PrimaryUserID {
			uid = "local-cn-user"
			session = "local-cn-session"
		}
		_ = json.NewEncoder(writer).Encode(map[string]any{
			"res_code":         0,
			"res_str":          "",
			"sess_key":         identity.SessionKey,
			"api_url":          baseURL + "/",
			"res_patch_url":    patchURL,
			"version_url":      versionURL,
			"res_img_url":      imageURL,
			"res_cpk_url":      cpkURL,
			"web_url":          baseURL + "/disabled/web",
			"charge_url":       baseURL + "/disabled/charge",
			"products_url":     baseURL + "/disabled/products",
			"update_url":       cnClientReleaseURL,
			"gid":              1,
			"userid":           identity.UserID,
			"uid":              uid,
			"session":          session,
			"realname_status":  1,
			"room_config":      nil,
			"comment_url":      nil,
			"display_pictures": map[string]any{},
			"sp_resource_flag": 1,
		})
	}
}

func normalizeLeadingSlashes(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if strings.HasPrefix(request.URL.Path, "//") {
			request.URL.Path = "/" + strings.TrimLeft(request.URL.Path, "/")
		}
		next.ServeHTTP(writer, request)
	})
}

func acknowledgeClientLog(writer http.ResponseWriter, request *http.Request) {
	response := make(map[string]string)
	var payload map[string]any
	if err := json.NewDecoder(request.Body).Decode(&payload); err == nil {
		if category, ok := payload["log_cat"].(string); ok && category != "" {
			response["log_cat"] = category
		}
	}
	writer.Header().Set("Content-Type", "application/json; charset=utf-8")
	writer.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(writer).Encode(response)
}
