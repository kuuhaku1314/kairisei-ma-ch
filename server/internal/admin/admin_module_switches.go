package admin

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"

	"kairisei.local/server/internal/accountstore"
	"kairisei.local/server/internal/protocol"
)

const moduleSwitchesKey = "module-switches"

func compileDisabledModules(keys []string) (protocol.ModuleSwitch, error) {
	definitions := protocol.OperatorModuleSwitches()
	var mask protocol.ModuleSwitch
	for _, key := range keys {
		i := slices.IndexFunc(definitions, func(d protocol.ModuleSwitchDefinition) bool { return d.Key == key })
		if i < 0 {
			return 0, fmt.Errorf("未知的页面功能开关 %q", key)
		}
		mask |= definitions[i].Bit
	}
	return mask, nil
}

func (o *Operations) loadModuleSwitches() error {
	doc, err := o.storage.ReadDocument(moduleSwitchesKey)
	if err != nil || doc.Revision == 0 {
		return err
	}
	var keys []string
	if err := json.Unmarshal(doc.Payload, &keys); err != nil {
		return fmt.Errorf("decode module switches: %w", err)
	}
	mask, err := compileDisabledModules(keys)
	if err != nil {
		return err
	}
	o.disabledModules, o.moduleSwitchesRevision = mask, doc.Revision
	return nil
}

func (admin *API) moduleSwitches(w http.ResponseWriter, _ *http.Request) {
	o := admin.operations
	o.configMu.RLock()
	mask, revision := o.disabledModules, o.moduleSwitchesRevision
	o.configMu.RUnlock()
	writeModuleSwitches(w, mask, revision)
}

func (admin *API) setModuleSwitches(w http.ResponseWriter, r *http.Request) {
	if err := requireAdminMutation(r); err != nil {
		WriteAdminError(w, http.StatusForbidden, err.Error())
		return
	}
	var body struct {
		Disabled []string `json:"disabled"`
		Expected *int     `json:"expected_revision"`
	}
	if err := DecodeAdminJSONLimit(r, &body, 8*1024); err != nil || body.Disabled == nil || body.Expected == nil {
		WriteAdminError(w, http.StatusBadRequest, "请提交关闭的功能数组及配置版本")
		return
	}
	mask, err := compileDisabledModules(body.Disabled)
	if err != nil {
		WriteAdminError(w, http.StatusBadRequest, err.Error())
		return
	}
	keys := []string{}
	for _, module := range protocol.OperatorModuleSwitches() {
		if mask&module.Bit != 0 {
			keys = append(keys, module.Key)
		}
	}
	o := admin.operations
	o.configMu.Lock()
	doc, err := o.storage.WriteDocument(moduleSwitchesKey, *body.Expected, keys, moduleSwitchesKey)
	if err == nil {
		o.disabledModules, o.moduleSwitchesRevision = mask, doc.Revision
		o.publishRuntimeLocked()
	}
	o.configMu.Unlock()
	switch {
	case errors.Is(err, accountstore.ErrDocumentConflict):
		WriteAdminError(w, http.StatusConflict, "页面功能开关已被更新，请刷新后重试")
	case err != nil:
		WriteAdminError(w, http.StatusInternalServerError, err.Error())
	default:
		writeModuleSwitches(w, mask, doc.Revision)
	}
}

func writeModuleSwitches(w http.ResponseWriter, disabled protocol.ModuleSwitch, revision int) {
	type moduleView struct {
		Key     string `json:"key"`
		Label   string `json:"label"`
		Enabled bool   `json:"enabled"`
	}
	definitions := protocol.OperatorModuleSwitches()
	modules := make([]moduleView, 0, len(definitions))
	for _, d := range definitions {
		modules = append(modules, moduleView{Key: d.Key, Label: d.Label, Enabled: disabled&d.Bit == 0})
	}
	WriteAdminJSON(w, http.StatusOK, map[string]any{"state": "PASS", "modules": modules, "revision": revision})
}
