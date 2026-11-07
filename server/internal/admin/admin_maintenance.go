package admin

import (
	"context"
	"errors"
	"net/http"
	"time"

	"kairisei.local/server/internal/accountstore"
	operationservice "kairisei.local/server/internal/operations"
)

func (a *API) maintenanceWrites(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet || r.Method == http.MethodHead || r.URL.Path == "/api/maintenance" || r.URL.Path == "/api/maintenance/cleanup" || r.URL.Path == "/api/maintenance/battles/interrupt" {
			next.ServeHTTP(w, r)
			return
		}
		finish, ok := a.operations.AdmitAdminMutation()
		if !ok {
			WriteAdminError(w, 409, "维护操作正在进行，请完成后再修改配置／账号")
			return
		}
		defer finish()
		next.ServeHTTP(w, r)
	})
}

func (a *API) getMaintenance(w http.ResponseWriter, _ *http.Request) {
	rooms, creating := a.multiplayerHub.MaintenanceActivity()
	WriteAdminJSON(w, 200, map[string]any{"state": "PASS", "maintenance": a.operations.MaintenanceState(), "rooms": rooms, "creating_rooms": creating, "gacha_trash": a.operations.gachaTrash()})
}

func (a *API) interruptBattles(w http.ResponseWriter, r *http.Request) {
	if err := requireAdminMutation(r); err != nil {
		WriteAdminError(w, 403, err.Error())
		return
	}
	var body struct {
		Expected *int `json:"expected_revision"`
	}
	if decodeAdminJSON(r, &body) != nil || body.Expected == nil {
		WriteAdminError(w, 400, "缺少维护版本，请刷新后重试")
		return
	}
	runtime, ok := a.business.(interface {
		WithAccountMaintenance(context.Context, func() error) error
	})
	if !ok {
		WriteAdminError(w, 503, "当前运行入口不支持中断对局")
		return
	}
	closed, solo := 0, 0
	err := a.operations.InterruptBattles(r.Context(), *body.Expected, func(ctx context.Context) error {
		var err error
		closed, err = a.multiplayerHub.InterruptRooms(ctx)
		if err != nil {
			return err
		}
		// Drop both serving-lifetime solo sessions and cached account handlers
		// after accepted settlements finish. Durable receipts remain available.
		return runtime.WithAccountMaintenance(ctx, func() error {
			solo = a.accounts.DiscardUnsettledBattles()
			return nil
		})
	})
	if errors.Is(err, operationservice.ErrMaintenanceConflict) {
		WriteAdminError(w, 409, err.Error())
		return
	}
	if err != nil {
		writeContentError(w, err)
		return
	}
	WriteAdminJSON(w, 200, map[string]any{"state": "PASS", "closed_rooms": closed, "discarded_solo": solo, "maintenance": a.operations.MaintenanceState()})
}

func (a *API) setMaintenance(w http.ResponseWriter, r *http.Request) {
	if err := requireAdminMutation(r); err != nil {
		WriteAdminError(w, 403, err.Error())
		return
	}
	var body struct {
		Enabled  bool `json:"enabled"`
		Expected *int `json:"expected_revision"`
	}
	if decodeAdminJSON(r, &body) != nil || body.Expected == nil {
		WriteAdminError(w, 400, "缺少维护状态或版本，请刷新后重试")
		return
	}
	state, err := a.operations.SetMaintenance(body.Enabled, *body.Expected, a.multiplayerHub.SetMaintenance)
	if errors.Is(err, operationservice.ErrMaintenanceConflict) {
		WriteAdminError(w, 409, err.Error())
		return
	}
	if err != nil {
		writeContentError(w, err)
		return
	}
	WriteAdminJSON(w, 200, map[string]any{"state": "PASS", "maintenance": state})
}

func (a *API) cleanupReferences(ctx context.Context) (accountstore.OperationalReferences, error) {
	refs := accountstore.OperationalReferences{Gachas: map[int]bool{}, Items: map[int]bool{}, Events: map[int]bool{}, Trades: map[int]bool{}}
	o := a.operations
	catalog, err := o.storage.CatalogState()
	if err != nil {
		return refs, err
	}
	for _, g := range catalog.Gachas {
		refs.Gachas[g.GachaID] = true
	}
	for id := range o.gachaBases {
		refs.Gachas[id] = true
	}
	ids, err := o.storage.RetainedGachaDocumentIDs(ctx)
	if err != nil {
		return refs, err
	}
	for _, id := range ids {
		refs.Gachas[id] = true
	}
	for _, tab := range catalog.ItemShopTabs {
		for _, line := range tab.Lineup {
			refs.Items[line.LineupID] = true
		}
	}
	for _, line := range o.runtimeSettings.ItemShop {
		refs.Items[line.LineupID] = true
	}
	for _, shop := range catalog.EventShopProfiles {
		for _, line := range shop.Lineups {
			refs.Events[line.LineupID] = true
		}
	}
	for _, shop := range catalog.TradeShopProfiles {
		for _, line := range shop.Lineups {
			refs.Trades[line.LineupID] = true
		}
	}
	if o.content == nil {
		return refs, errors.New("运营目录尚未完整载入，不能判断无引用状态")
	}
	for _, shop := range o.content.shops {
		for _, line := range shop.Lineups {
			refs.Trades[line.LineupID] = true
		}
	}
	return refs, nil
}

func (a *API) cleanupOperationalState(w http.ResponseWriter, r *http.Request) {
	if err := requireAdminMutation(r); err != nil {
		WriteAdminError(w, 403, err.Error())
		return
	}
	var body struct {
		Scope    string                             `json:"scope"`
		Mode     string                             `json:"mode"`
		Digest   string                             `json:"digest"`
		StartUTC string                             `json:"start_utc"`
		EndUTC   string                             `json:"end_utc"`
		GroupID  int                                `json:"group_id"`
		History  accountstore.HistoryCleanupRequest `json:"history"`
	}
	if decodeAdminJSON(r, &body) != nil || (body.Mode != "preview" && body.Mode != "apply") || (body.Mode == "apply" && len(body.Digest) != 64) {
		WriteAdminError(w, 400, "请先预览并提交对应清理摘要")
		return
	}
	if body.Scope == "" {
		body.Scope = "operational"
	}
	if body.Scope != "operational" && body.Scope != "audit" && body.Scope != "gacha" && body.Scope != "history" {
		WriteAdminError(w, 400, "不支持的清理范围")
		return
	}
	if body.Scope == "gacha" && body.GroupID <= 0 {
		WriteAdminError(w, 400, "请选择回收站中的卡池")
		return
	}
	if body.Scope == "audit" {
		var err error
		body.StartUTC, body.EndUTC, err = accountstore.AuditCleanupRange(body.StartUTC, body.EndUTC)
		if err != nil {
			WriteAdminError(w, 400, err.Error())
			return
		}
	}
	if body.Scope == "history" {
		if err := body.History.Normalize(time.Now()); err != nil {
			WriteAdminError(w, 400, err.Error())
			return
		}
	}
	runtime, ok := a.business.(interface {
		WithAccountMaintenance(context.Context, func() error) error
	})
	if !ok {
		WriteAdminError(w, 503, "当前运行入口不支持维护清理")
		return
	}
	err := a.operations.RunCleanup(operationservice.CleanupJob{Scope: body.Scope, Mode: body.Mode, GroupID: body.GroupID}, func(ctx context.Context, job *operationservice.CleanupJob) error {
		if rooms, creating := a.multiplayerHub.MaintenanceActivity(); rooms != 0 || creating != 0 {
			return errors.New("仍有组队房间或建房请求，请先中断未结算对局")
		}
		return runtime.WithAccountMaintenance(ctx, func() error {
			expected := ""
			if body.Mode == "apply" {
				expected = body.Digest
			}
			if body.Scope == "gacha" {
				report, err := a.purgeGacha(ctx, body.GroupID, expected)
				job.Report = &report
				return err
			}
			if body.Scope == "history" {
				report, err := a.operations.storage.CleanupHistory(ctx, body.History, expected)
				job.HistoryReport = &report
				return err
			}
			a.operations.configMu.RLock()
			defer a.operations.configMu.RUnlock()
			if body.Scope == "audit" {
				report, err := a.operations.storage.CleanupAuditHistory(ctx, body.StartUTC, body.EndUTC, expected)
				job.AuditReport = &report
				return err
			}
			refs, err := a.cleanupReferences(ctx)
			if err != nil {
				return err
			}
			report, err := a.operations.storage.CleanupOperationalState(ctx, refs, expected)
			job.Report = &report
			return err
		})
	})
	if err != nil {
		WriteAdminError(w, 409, err.Error())
		return
	}
	WriteAdminJSON(w, http.StatusAccepted, map[string]any{"state": "PASS", "maintenance": a.operations.MaintenanceState()})
}

// getMaintenanceHistory summarizes cleanable history sizes; it is read-only and
// available outside maintenance so operators can judge when to clean.
func (a *API) getMaintenanceHistory(w http.ResponseWriter, r *http.Request) {
	overview, err := a.operations.storage.HistoryOverview(r.Context())
	if err != nil {
		WriteAdminError(w, 500, err.Error())
		return
	}
	bosses := make([]map[string]any, 0, len(overview.RankRows))
	a.operations.configMu.RLock()
	scoring := a.scoreRankingBossesLocked()
	for _, row := range overview.RankRows {
		name := ""
		if content := a.operations.content; content != nil {
			if boss, ok := content.bosses[row.BossID]; ok {
				name = boss.Name
				if boss.Difficulty != "" {
					name += " · " + boss.Difficulty
				}
			}
		}
		_, open := scoring[row.BossID]
		bosses = append(bosses, map[string]any{"boss_id": row.BossID, "rows": row.Rows, "name": name, "scoring": open})
	}
	a.operations.configMu.RUnlock()
	WriteAdminJSON(w, 200, map[string]any{"state": "PASS", "overview": overview, "rank_bosses": bosses})
}
