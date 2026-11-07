package admin

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"kairisei.local/server/internal/accountstore"
)

func (admin *API) changeLoginBlock(w http.ResponseWriter, r *http.Request) {
	if err := requireAdminMutation(r); err != nil {
		WriteAdminError(w, http.StatusForbidden, err.Error())
		return
	}
	userID, err := strconv.Atoi(chi.URLParam(r, "userID"))
	if err != nil || userID < accountstore.PrimaryUserID || userID >= accountstore.SystemPartnerUserIDBase {
		WriteAdminError(w, http.StatusBadRequest, "无效的玩家 ID")
		return
	}
	var until *int64
	if r.Method == http.MethodPut {
		var body *struct {
			UntilUnix int64 `json:"until_unix"`
		}
		if err := DecodeAdminJSONLimit(r, &body, 1024); err != nil || body == nil {
			WriteAdminError(w, http.StatusBadRequest, "封禁设置格式无效")
			return
		}
		until = &body.UntilUnix
	}
	block, err := admin.accounts.SetLoginBlock(r.Context(), userID, until)
	switch {
	case errors.Is(err, accountstore.ErrLoginBlockAccountNotFound):
		WriteAdminError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, accountstore.ErrLoginBlockExpiry):
		WriteAdminError(w, http.StatusBadRequest, err.Error())
	case err != nil:
		WriteAdminError(w, http.StatusInternalServerError, err.Error())
	default:
		WriteAdminJSON(w, http.StatusOK, map[string]any{"state": "PASS", "user_id": userID, "login_block": block})
	}
}
