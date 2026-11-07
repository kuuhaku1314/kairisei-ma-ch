package admin

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"kairisei.local/server/internal/accountstore"
)

const loginWhitelistKey = "login-whitelist"

type loginWhitelistConfig struct {
	Enabled bool  `json:"enabled"`
	UserIDs []int `json:"user_ids"`
}

func (o *Operations) loadLoginWhitelist() error {
	doc, err := o.storage.ReadDocument(loginWhitelistKey)
	if err != nil || doc.Revision == 0 {
		return err
	}
	var stored loginWhitelistConfig
	if err := json.Unmarshal(doc.Payload, &stored); err != nil {
		return fmt.Errorf("decode login whitelist: %w", err)
	}
	policy, err := accountstore.CompileLoginWhitelist(stored.Enabled, stored.UserIDs)
	if err != nil {
		return err
	}
	o.loginWhitelist, o.loginWhitelistRevision = policy, doc.Revision
	return nil
}

func (admin *API) loginWhitelist(w http.ResponseWriter, _ *http.Request) {
	o := admin.operations
	o.configMu.RLock()
	policy, revision := o.loginWhitelist, o.loginWhitelistRevision
	o.configMu.RUnlock()
	writeLoginWhitelist(w, loginWhitelistConfig{Enabled: policy.Enabled(), UserIDs: policy.UserIDs()}, revision)
}

func (admin *API) setLoginWhitelist(w http.ResponseWriter, r *http.Request) {
	if err := requireAdminMutation(r); err != nil {
		WriteAdminError(w, http.StatusForbidden, err.Error())
		return
	}
	var body struct {
		Enabled  *bool `json:"enabled"`
		UserIDs  []int `json:"user_ids"`
		Expected *int  `json:"expected_revision"`
	}
	if err := DecodeAdminJSONLimit(r, &body, 64*1024); err != nil || body.Enabled == nil || body.UserIDs == nil || body.Expected == nil {
		WriteAdminError(w, http.StatusBadRequest, "请提交白名单开关、玩家 ID 数组及配置版本")
		return
	}
	policy, err := accountstore.CompileLoginWhitelist(*body.Enabled, body.UserIDs)
	if err != nil {
		WriteAdminError(w, http.StatusBadRequest, err.Error())
		return
	}
	config := loginWhitelistConfig{Enabled: policy.Enabled(), UserIDs: policy.UserIDs()}
	if len(config.UserIDs) != 0 {
		count, err := admin.accounts.CountIDs(config.UserIDs)
		if err != nil {
			WriteAdminError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if count != len(config.UserIDs) {
			WriteAdminError(w, http.StatusBadRequest, "登录白名单包含不存在的玩家 ID")
			return
		}
	}
	o := admin.operations
	o.configMu.Lock()
	doc, err := o.storage.WriteDocument(loginWhitelistKey, *body.Expected, config, loginWhitelistKey)
	if err == nil {
		o.loginWhitelist, o.loginWhitelistRevision = policy, doc.Revision
		o.publishRuntimeLocked()
	}
	o.configMu.Unlock()
	switch {
	case errors.Is(err, accountstore.ErrDocumentConflict):
		WriteAdminError(w, http.StatusConflict, "登录白名单已被更新，请刷新后重试")
	case err != nil:
		WriteAdminError(w, http.StatusInternalServerError, err.Error())
	default:
		writeLoginWhitelist(w, config, doc.Revision)
	}
}

func writeLoginWhitelist(w http.ResponseWriter, config loginWhitelistConfig, revision int) {
	WriteAdminJSON(w, http.StatusOK, map[string]any{"state": "PASS", "enabled": config.Enabled, "user_ids": config.UserIDs, "revision": revision})
}
