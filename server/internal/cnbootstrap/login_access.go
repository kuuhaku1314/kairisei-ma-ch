package cnbootstrap

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"kairisei.local/server/internal/accountstore"
)

const ipBlacklistMessage = "当前网络地址已被服务器禁止访问"

func writeAccountAccessRejection(w http.ResponseWriter, err error, login bool) bool {
	var block *accountstore.LoginBlockedError
	switch {
	case errors.As(err, &block):
		writeAccessDenial(w, login, -12005, "account_login_blocked", block.Error(), &block.UntilUnix)
	case errors.Is(err, accountstore.ErrLoginWhitelist):
		writeAccessDenial(w, login, -12007, "login_whitelist_required", err.Error(), nil)
	default:
		return false
	}
	return true
}

func writeAccessDenial(w http.ResponseWriter, login bool, resultCode int, errorCode, message string, until *int64) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	response := map[string]any{"error_code": errorCode}
	if until != nil {
		response["until_unix"] = *until
	}
	if login {
		response["res_code"], response["res_str"] = resultCode, message
	} else {
		response["state"], response["error"] = "FAIL", message
		w.WriteHeader(http.StatusForbidden)
	}
	_ = json.NewEncoder(w).Encode(response)
}

func (app *application) blockIPRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" || !app.operations.IPBlocked(r.RemoteAddr) {
			next.ServeHTTP(w, r)
			return
		}
		path := strings.TrimLeft(r.URL.Path, "/")
		switch {
		case r.Method == http.MethodPost && path == "local/client/login":
			writeAccessDenial(w, true, -12006, "ip_blacklisted", ipBlacklistMessage, nil)
		case r.Method == http.MethodPost && strings.HasPrefix(path, "local/account/"):
			writeAccessDenial(w, false, -12006, "ip_blacklisted", ipBlacklistMessage, nil)
		case businessCompressionRoute(r):
			common := cnBootstrapCommon()
			common["res_code"] = -12006
			common["res_str"] = ipBlacklistMessage
			common["res_err_action"] = 1
			writeCNProtocolResponseWithPopup(w, common, map[string]any{})
		default:
			w.Header().Set("Cache-Control", "no-store")
			http.Error(w, ipBlacklistMessage, http.StatusForbidden)
		}
	})
}
