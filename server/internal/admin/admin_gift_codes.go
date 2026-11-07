package admin

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"kairisei.local/server/internal/accountstore"
	"kairisei.local/server/internal/game"
)

func (a *API) giftBatches(w http.ResponseWriter, r *http.Request) {
	registry, revision, err := a.operations.storage.GiftBatches()
	if err != nil {
		WriteAdminError(w, 500, err.Error())
		return
	}
	counts, err := a.operations.storage.GiftBatchCounts()
	if err != nil {
		WriteAdminError(w, 500, err.Error())
		return
	}
	WriteAdminJSON(w, 200, map[string]any{"revision": revision, "batches": registry.Batches, "counts": counts})
}

func (a *API) validateGiftBatch(b *game.GiftCodeBatch, requireReady bool) error {
	b.Name = strings.TrimSpace(b.Name)
	b.Title = strings.TrimSpace(b.Title)
	b.Message = strings.TrimSpace(b.Message)
	b.Group = strings.TrimSpace(b.Group)
	if b.Name == "" || len([]rune(b.Name)) > 80 || b.Title == "" || len([]rune(b.Title)) > 40 || b.Message == "" || len([]rune(b.Message)) > 200 {
		return errors.New("批次名称1–80字，邮件标题1–40字，正文1–200字")
	}
	if b.ClaimScope != "batch" && b.ClaimScope != "group" && b.ClaimScope != "code" {
		return errors.New("请选择按批次、按组或仅按码限领")
	}
	if requireReady && b.ClaimScope == "group" && b.Group == "" {
		return errors.New("按组限领须填写领取组")
	}
	if b.ClaimScope != "group" {
		b.Group = ""
	}
	if b.Mode != "public" && b.Mode != "single" {
		return errors.New("兑换模式须为公共码或单次码")
	}
	if len(b.Group) > 80 || b.Limit < 0 || b.StartsAt < 0 || b.EndsAt < 0 || b.EndsAt > 2147483647 || b.EndsAt != 0 && b.EndsAt <= b.StartsAt {
		return errors.New("限领组、额度或排期无效")
	}
	if requireReady && len(b.Rewards) < 1 || len(b.Rewards) > 10 {
		return errors.New("每次兑换须有1–10份奖励")
	}
	if !requireReady {
		return nil
	}
	for i, r := range b.Rewards {
		normalized, _, err := a.mailReward(AdminMailRequest{RewardType: r.Type, RewardTypeID: r.RewardTypeID, Quantity: r.Num, CardLevel: int(r.CardLevel), CardFame: int(r.CardFame), CardLove: r.CardLove})
		if err != nil {
			return err
		}
		b.Rewards[i] = normalized
	}
	return nil
}

func (a *API) saveGiftBatch(w http.ResponseWriter, r *http.Request) {
	if err := requireAdminMutation(r); err != nil {
		WriteAdminError(w, 403, err.Error())
		return
	}
	var input struct {
		ExpectedRevision *int               `json:"expected_revision"`
		Batch            game.GiftCodeBatch `json:"batch"`
		Preview          bool               `json:"preview"`
	}
	if err := decodeAdminJSON(r, &input); err != nil {
		WriteAdminError(w, 400, err.Error())
		return
	}
	if input.ExpectedRevision == nil {
		WriteAdminError(w, 400, "缺少配置版本")
		return
	}
	requireReady := input.Batch.Published
	if input.Batch.ID != 0 && !input.Batch.Enabled {
		registry, _, err := a.operations.storage.GiftBatches()
		if err != nil {
			WriteAdminError(w, 500, err.Error())
			return
		}
		for _, previous := range registry.Batches {
			if previous.ID == input.Batch.ID && previous.Published {
				requireReady = false
				break
			}
		}
	}
	if err := a.validateGiftBatch(&input.Batch, requireReady); err != nil {
		WriteAdminError(w, 400, err.Error())
		return
	}
	if input.Preview {
		WriteAdminJSON(w, 200, map[string]any{"batch": input.Batch})
		return
	}
	registry, revision, err := a.operations.storage.SaveGiftBatch(*input.ExpectedRevision, input.Batch)
	if err != nil {
		status := 400
		if errors.Is(err, accountstore.ErrDocumentConflict) {
			status = 409
		}
		WriteAdminError(w, status, err.Error())
		return
	}
	WriteAdminJSON(w, 200, map[string]any{"revision": revision, "batches": registry.Batches})
}

func (a *API) generateGiftCodes(w http.ResponseWriter, r *http.Request) {
	if err := requireAdminMutation(r); err != nil {
		WriteAdminError(w, 403, err.Error())
		return
	}
	var input accountstore.GiftCodeGeneration
	if err := decodeAdminJSON(r, &input); err != nil {
		WriteAdminError(w, 400, err.Error())
		return
	}
	ids, err := a.operations.storage.GenerateGiftCodes(input)
	if err != nil {
		status := 400
		if errors.Is(err, accountstore.ErrDocumentConflict) {
			status = 409
		}
		WriteAdminError(w, status, err.Error())
		return
	}
	WriteAdminJSON(w, 200, map[string]any{"count": len(ids), "ids": ids})
}

func (a *API) listGiftCodes(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(chi.URLParam(r, "batchID"))
	if err != nil || id <= 0 {
		WriteAdminError(w, 400, "批次无效")
		return
	}
	offset := 0
	if raw := r.URL.Query().Get("offset"); raw != "" {
		offset, err = strconv.Atoi(raw)
		if err != nil || offset < 0 {
			WriteAdminError(w, 400, "页码无效")
			return
		}
	}
	if r.URL.Query().Get("kind") == "redemptions" {
		rows, err := a.operations.storage.GiftRedemptions(id, 100, offset)
		if err != nil {
			WriteAdminError(w, 500, err.Error())
			return
		}
		WriteAdminJSON(w, 200, map[string]any{"rows": rows})
		return
	}
	rows, err := a.operations.storage.GiftCodes(id, 100, offset)
	if err != nil {
		WriteAdminError(w, 500, err.Error())
		return
	}
	WriteAdminJSON(w, 200, map[string]any{"rows": rows})
}

func (a *API) changeGiftCode(w http.ResponseWriter, r *http.Request) {
	if err := requireAdminMutation(r); err != nil {
		WriteAdminError(w, 403, err.Error())
		return
	}
	var input struct {
		ID       int64 `json:"id"`
		Expected *bool `json:"expected_enabled"`
		Enabled  bool  `json:"enabled"`
	}
	if err := decodeAdminJSON(r, &input); err != nil {
		WriteAdminError(w, 400, err.Error())
		return
	}
	if input.ID <= 0 || input.Expected == nil {
		WriteAdminError(w, 400, "缺少兑换码状态")
		return
	}
	if err := a.operations.storage.SetGiftCodeEnabled(input.ID, *input.Expected, input.Enabled); err != nil {
		WriteAdminError(w, 409, err.Error())
		return
	}
	WriteAdminJSON(w, 200, map[string]bool{"enabled": input.Enabled})
}
