package admin

import (
	"net/http"
	"sort"
	"strconv"

	"github.com/go-chi/chi/v5"

	"kairisei.local/server/internal/game"
	"kairisei.local/server/internal/gamestate"
)

// Retain deleted identities so restoring a shop cannot reset per-player limits.
func (c *contentStore) exchangeShops() []exchangeShop {
	merged := make(map[int]exchangeShop, len(c.base.TradeShopProfiles)+len(c.shops))
	for _, shop := range c.base.TradeShopProfiles {
		merged[shop.TradeShopID] = exchangeShop{TradeShopProfile: shop}
	}
	for id, shop := range c.shops {
		merged[id] = shop
	}
	result := make([]exchangeShop, 0, len(merged))
	for _, shop := range merged {
		result = append(result, shop)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].TradeShopID < result[j].TradeShopID })
	return result
}

func (c *contentStore) exchangeProfiles() []gamestate.TradeShopProfile {
	shops := c.exchangeShops()
	result := make([]gamestate.TradeShopProfile, 0, len(shops))
	for _, shop := range shops {
		result = append(result, shop.TradeShopProfile)
	}
	return result
}

func (a *API) changeExchangeDeletion(w http.ResponseWriter, r *http.Request) {
	if err := requireAdminMutation(r); err != nil {
		WriteAdminError(w, 403, err.Error())
		return
	}
	var body struct {
		Expected *int `json:"expected_revision"`
	}
	id, err := strconv.Atoi(chi.URLParam(r, "shopID"))
	if err != nil || decodeAdminJSON(r, &body) != nil || body.Expected == nil {
		WriteAdminError(w, 400, "缺少兑换所ID或配置版本")
		return
	}
	o := a.operations
	o.configMu.Lock()
	defer o.configMu.Unlock()
	c := o.content
	if c == nil {
		WriteAdminError(w, 503, "未载入兑换目录")
		return
	}
	var selected *exchangeShop
	for _, shop := range c.exchangeShops() {
		if shop.TradeShopID == id {
			selected = &shop
			break
		}
	}
	if selected == nil {
		WriteAdminError(w, 404, "兑换所不存在")
		return
	}
	if selected.Deleted == (r.Method == http.MethodDelete) {
		WriteAdminError(w, 409, "兑换所状态已变化，请重新载入")
		return
	}
	selected.Deleted = r.Method == http.MethodDelete
	selected.Disabled = true
	next := make(map[int]exchangeShop, len(c.shops)+1)
	for id, shop := range c.shops {
		next[id] = shop
	}
	next[id] = *selected
	state, err := c.project(c.drops, next, c.rules)
	if err != nil {
		writeContentError(w, err)
		return
	}
	if len(state.TradeShopProfiles) > 50 {
		WriteAdminError(w, 400, "最多50个未删除的兑换所")
		return
	}
	configuration, err := game.NewContentConfiguration(c.configuration.Revision+1, state)
	if err != nil {
		WriteAdminError(w, 400, err.Error())
		return
	}
	doc, err := o.writeDocument(exchangesKey, *body.Expected, next)
	if err != nil {
		writeContentError(w, err)
		return
	}
	c.shops, c.shopRevision = next, doc.Revision
	c.configuration = configuration
	o.publishRuntimeLocked()
	WriteAdminJSON(w, 200, map[string]any{"state": "PASS", "revision": doc.Revision})
}
