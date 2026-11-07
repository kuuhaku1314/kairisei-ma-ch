package admin

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"kairisei.local/server/internal/accountstore"
	"kairisei.local/server/internal/operations"
)

const ipBlacklistKey = "ip-blacklist"

func (o *Operations) loadIPBlacklist() error {
	doc, err := o.storage.ReadDocument(ipBlacklistKey)
	if err != nil || doc.Revision == 0 {
		return err
	}
	var entries []string
	if err := json.Unmarshal(doc.Payload, &entries); err != nil {
		return fmt.Errorf("decode IP blacklist: %w", err)
	}
	policy, err := operations.CompileIPBlacklist(entries)
	if err != nil {
		return err
	}
	o.ipBlacklist, o.ipBlacklistRevision = policy, doc.Revision
	return nil
}

func (admin *API) ipBlacklist(w http.ResponseWriter, _ *http.Request) {
	o := admin.operations
	o.configMu.RLock()
	policy, revision := o.ipBlacklist, o.ipBlacklistRevision
	o.configMu.RUnlock()
	writeIPBlacklist(w, policy.Entries(), revision)
}

func (admin *API) setIPBlacklist(w http.ResponseWriter, r *http.Request) {
	if err := requireAdminMutation(r); err != nil {
		WriteAdminError(w, http.StatusForbidden, err.Error())
		return
	}
	var body struct {
		Entries  []string `json:"entries"`
		Expected *int     `json:"expected_revision"`
	}
	if err := DecodeAdminJSONLimit(r, &body, 64*1024); err != nil || body.Entries == nil || body.Expected == nil {
		WriteAdminError(w, http.StatusBadRequest, "请提交 IP 黑名单数组及配置版本")
		return
	}
	policy, err := operations.CompileIPBlacklist(body.Entries)
	if err != nil {
		WriteAdminError(w, http.StatusBadRequest, err.Error())
		return
	}
	entries := policy.Entries()
	o := admin.operations
	o.configMu.Lock()
	doc, err := o.storage.WriteDocument(ipBlacklistKey, *body.Expected, entries, ipBlacklistKey)
	if err == nil {
		o.ipBlacklist, o.ipBlacklistRevision = policy, doc.Revision
		o.publishRuntimeLocked()
	}
	o.configMu.Unlock()
	switch {
	case errors.Is(err, accountstore.ErrDocumentConflict):
		WriteAdminError(w, http.StatusConflict, "IP 黑名单已被更新，请刷新后重试")
	case err != nil:
		WriteAdminError(w, http.StatusInternalServerError, err.Error())
	default:
		writeIPBlacklist(w, entries, doc.Revision)
	}
}

func writeIPBlacklist(w http.ResponseWriter, entries []string, revision int) {
	WriteAdminJSON(w, http.StatusOK, map[string]any{"state": "PASS", "entries": entries, "revision": revision})
}
