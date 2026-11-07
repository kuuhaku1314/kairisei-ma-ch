package admin

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"kairisei.local/server/internal/accountstore"
	"kairisei.local/server/internal/gamestate"
)

const adminBatchPrefix = accountstore.MailBatchPrefix

type AdminMailBatch struct {
	ID             string              `json:"batch_id"`
	UserIDs        []int               `json:"user_ids"`
	Title          string              `json:"title"`
	Message        string              `json:"message"`
	Rewards        []AdminMailRequest  `json:"rewards"`
	AllPlayers     bool                `json:"all_players,omitempty"`
	RecipientRange *mailRecipientRange `json:"recipient_range,omitempty"`
}

func validBatchID(id string) bool {
	if len(id) < 8 || len(id) > 80 {
		return false
	}
	for _, c := range id {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-') {
			return false
		}
	}
	return true
}

func (admin *API) validateMailBatch(batch *AdminMailBatch) ([]AdminCatalogEntry, error) {
	if batch.AllPlayers != (batch.RecipientRange != nil) || batch.RecipientRange != nil && !batch.RecipientRange.valid() {
		return nil, errors.New("全员发放范围无效，请重新预览")
	}
	batch.Title, batch.Message = strings.TrimSpace(batch.Title), strings.TrimSpace(batch.Message)
	if !validBatchID(batch.ID) || len(batch.UserIDs) < 1 || (!batch.AllPlayers && len(batch.UserIDs) > 500) || len(batch.Rewards) < 1 || len(batch.Rewards) > 120 || len([]rune(batch.Title)) < 1 || len([]rune(batch.Title)) > 40 || len([]rune(batch.Message)) < 1 || len([]rune(batch.Message)) > 200 {
		return nil, errors.New("请填写标题、正文和1–120种奖励；手动选择1–500名玩家，或选择全部玩家")
	}
	slices.Sort(batch.UserIDs)
	batch.UserIDs = slices.Compact(batch.UserIDs)
	for _, id := range batch.UserIDs {
		if id < accountstore.PrimaryUserID || id >= accountstore.SystemPartnerUserIDBase {
			return nil, fmt.Errorf("玩家 #%d 无效或属于系统伙伴", id)
		}
	}
	// Keep SQLite bound-parameter counts independent of the audience size.
	for offset := 0; offset < len(batch.UserIDs); offset += 500 {
		ids := batch.UserIDs[offset:min(offset+500, len(batch.UserIDs))]
		count, err := admin.accounts.CountIDs(ids)
		if err != nil {
			return nil, err
		}
		if count != len(ids) {
			return nil, errors.New("收件名单包含不存在的玩家，请重新选择")
		}
	}
	catalog := make([]AdminCatalogEntry, 0, len(batch.Rewards))
	seen := make(map[string]bool)
	for i, input := range batch.Rewards {
		// Title, content and IDs belong to the batch, never to an item.
		if input.Discardable || input.IdempotencyKey != "" || input.Title != "" || input.Message != "" {
			return nil, errors.New("批量奖励只能配置物品、数量及卡牌属性")
		}
		reward, entry, err := admin.mailReward(input)
		if err != nil {
			return nil, fmt.Errorf("第%d种奖励：%w", i+1, err)
		}
		key := adminCatalogKey(reward.Type, reward.RewardTypeID)
		if seen[key] {
			return nil, errors.New("同一种奖励请合并数量后发放")
		}
		seen[key] = true
		input.CardLevel, input.CardFame, input.CardLove = int(reward.CardLevel), int(reward.CardFame), reward.CardLove
		batch.Rewards[i] = input
		catalog = append(catalog, entry)
	}
	return catalog, nil
}

func (admin *API) previewMailBatch(w http.ResponseWriter, r *http.Request) {
	if err := requireAdminMutation(r); err != nil {
		WriteAdminError(w, 403, err.Error())
		return
	}
	var batch AdminMailBatch
	if err := decodeAdminJSON(r, &batch); err != nil {
		WriteAdminError(w, 400, err.Error())
		return
	}
	if batch.AllPlayers {
		if len(batch.UserIDs) != 0 || batch.RecipientRange != nil {
			WriteAdminError(w, 400, "全部玩家与手动名单不能同时指定")
			return
		}
		var err error
		batch.UserIDs, err = admin.accounts.PlayerIDs(r.Context(), accountstore.PrimaryUserID, accountstore.SystemPartnerUserIDBase-1)
		if err != nil {
			WriteAdminError(w, 500, err.Error())
			return
		}
		if len(batch.UserIDs) != 0 {
			batch.RecipientRange = &mailRecipientRange{StartUID: batch.UserIDs[0], EndUID: batch.UserIDs[len(batch.UserIDs)-1], Count: len(batch.UserIDs)}
		}
	}
	catalog, err := admin.validateMailBatch(&batch)
	if err != nil {
		WriteAdminError(w, 400, err.Error())
		return
	}
	count := len(batch.UserIDs)
	if batch.AllPlayers {
		// New registrations get a UID above this preview's upper bound.
		// Keep only the range in the request, not an unbounded client-side list.
		batch.UserIDs = nil
	}
	WriteAdminJSON(w, 200, map[string]any{"state": "PASS", "batch": batch, "confirmation": batch, "catalog": catalog, "recipient_count": count, "mail_count": count * len(batch.Rewards)})
}

func (admin *API) createMailBatch(w http.ResponseWriter, r *http.Request) {
	if err := requireAdminMutation(r); err != nil {
		WriteAdminError(w, 403, err.Error())
		return
	}
	var batch AdminMailBatch
	if err := decodeAdminJSON(r, &batch); err != nil {
		WriteAdminError(w, 400, err.Error())
		return
	}
	if batch.AllPlayers {
		if batch.RecipientRange == nil || !batch.RecipientRange.valid() || len(batch.UserIDs) != 0 {
			WriteAdminError(w, 400, "全员发放范围无效，请重新预览")
			return
		}
		var err error
		batch.UserIDs, err = admin.accounts.PlayerIDs(r.Context(), batch.RecipientRange.StartUID, batch.RecipientRange.EndUID)
		if err != nil {
			WriteAdminError(w, 500, err.Error())
			return
		}
		if len(batch.UserIDs) != batch.RecipientRange.Count {
			WriteAdminError(w, 409, "预览范围内的账号发生变化，请重新预览")
			return
		}
	}
	if _, err := admin.validateMailBatch(&batch); err != nil {
		WriteAdminError(w, 400, err.Error())
		return
	}
	// A cleaned batch keeps no receipts, so recreating it would resend every gift.
	if retired, err := admin.operations.storage.MailBatchRetired(batch.ID); err != nil || retired {
		if err == nil {
			err = errors.New("该批次已完成并在维护中清理了历史，不能再次发放；如需补发请新建批次")
		}
		WriteAdminError(w, 409, err.Error())
		return
	}
	content, _ := json.Marshal(batch)
	doc, err := admin.operations.writeDocument(adminBatchPrefix+batch.ID, 0, batch)
	if errors.Is(err, accountstore.ErrDocumentConflict) {
		doc, err = admin.operations.storage.ReadDocument(adminBatchPrefix + batch.ID)
		if err == nil && !bytes.Equal(doc.Payload, content) {
			WriteAdminError(w, 409, "批次编号已用于其他内容，请重新预览")
			return
		}
	}
	if err != nil {
		WriteAdminError(w, 500, err.Error())
		return
	}
	WriteAdminJSON(w, 200, map[string]any{"state": "PASS", "batch_id": batch.ID, "created_utc": doc.UpdatedUTC})
}

func (admin *API) readMailBatch(id string) (AdminMailBatch, accountstore.Document, error) {
	if !validBatchID(id) {
		return AdminMailBatch{}, accountstore.Document{}, errors.New("批次编号无效")
	}
	doc, err := admin.operations.storage.ReadDocument(adminBatchPrefix + id)
	var batch AdminMailBatch
	if err == nil && doc.Revision == 0 {
		err = errors.New("批次不存在")
	}
	if err == nil {
		err = json.Unmarshal(doc.Payload, &batch)
	}
	return batch, doc, err
}

func (admin *API) mailBatchStatus(w http.ResponseWriter, r *http.Request) {
	batch, doc, err := admin.readMailBatch(chi.URLParam(r, "batchID"))
	if err != nil {
		WriteAdminError(w, 404, err.Error())
		return
	}
	seen, err := admin.operations.storage.DeliveredUsers(adminBatchPrefix+batch.ID, doc.SHA256)
	if err != nil {
		WriteAdminError(w, 500, err.Error())
		return
	}
	done, pending := []int{}, []int{}
	for _, id := range batch.UserIDs {
		if seen[id] {
			done = append(done, id)
		} else {
			pending = append(pending, id)
		}
	}
	WriteAdminJSON(w, 200, map[string]any{"state": "PASS", "batch": batch, "created_utc": doc.UpdatedUTC, "done": done, "pending": pending})
}

func (admin *API) deliverMailBatchUser(batch AdminMailBatch, doc accountstore.Document, userID int) (json.RawMessage, error) {
	lock := admin.business.AccountLock(userID)
	lock.Lock()
	defer lock.Unlock()
	key := adminBatchPrefix + batch.ID
	if receipt, err := admin.operations.storage.ActionReceipt(key, userID, doc.SHA256); err != nil || receipt != nil {
		return receipt, err
	}
	state, err := admin.loadAccountState(userID)
	if err != nil {
		return nil, err
	}
	if len(state.Engagement.Presents)+len(batch.Rewards) > 30000 {
		return nil, errors.New("礼物箱过满，请先领取后重试")
	}
	presentIDs := make([]int64, 0, len(batch.Rewards))
	issued := time.Now().Unix()
	for i, input := range batch.Rewards {
		reward, _, err := admin.mailReward(input)
		if err != nil {
			return nil, err
		}
		itemKey := fmt.Sprintf("%s:%d", batch.ID, i)
		presentID := adminMailPresentID(userID, itemKey)
		for _, list := range [][]gamestate.Present{state.Engagement.Presents, state.Engagement.Histories} {
			for _, existing := range list {
				if existing.PresentID == presentID {
					return nil, errors.New("礼物编号冲突，请核对原批次")
				}
			}
		}
		empty := gamestate.Reward{CardSkillLevels: []int16{}}
		state.Engagement.Presents = append(state.Engagement.Presents, gamestate.Present{PresentID: presentID, IssuedAtUnix: issued, Reward: reward, Reward0: empty, Reward1: empty, Reward2: empty, Title: batch.Title, Comment: batch.Message, AdminIdempotencyKey: itemKey})
		presentIDs = append(presentIDs, presentID)
	}
	result := map[string]any{"user_id": userID, "present_ids": presentIDs, "batch_id": batch.ID}
	if err := admin.accounts.PersistStateWithAudit(userID, state, &accountstore.AdminAudit{
		Operation: "mail-delivery", Target: strconv.Itoa(userID), Payload: result,
		Receipt: &accountstore.AdminActionReceipt{OperationKey: key, UserID: userID, RequestSHA256: doc.SHA256, Result: result},
	}); err != nil {
		return nil, err
	}
	admin.business.Invalidate(userID)
	content, err := json.Marshal(result)
	return content, err
}

func (admin *API) runMailBatch(w http.ResponseWriter, r *http.Request) {
	if err := requireAdminMutation(r); err != nil {
		WriteAdminError(w, 403, err.Error())
		return
	}
	batch, doc, err := admin.readMailBatch(chi.URLParam(r, "batchID"))
	if err != nil {
		WriteAdminError(w, 404, err.Error())
		return
	}
	var body struct {
		UserIDs []int `json:"user_ids"`
	}
	if err := decodeAdminJSON(r, &body); err != nil || len(body.UserIDs) < 1 || len(body.UserIDs) > 10 {
		WriteAdminError(w, 400, "每次推进1–10名玩家")
		return
	}
	for _, id := range body.UserIDs {
		if !slices.Contains(batch.UserIDs, id) {
			WriteAdminError(w, 400, "玩家不属于该批次")
			return
		}
	}
	results := make([]map[string]any, 0, len(body.UserIDs))
	for _, id := range body.UserIDs {
		if r.Context().Err() != nil {
			break
		}
		result, err := admin.deliverMailBatchUser(batch, doc, id)
		if err != nil {
			results = append(results, map[string]any{"user_id": id, "ok": false, "error": err.Error()})
		} else {
			results = append(results, map[string]any{"user_id": id, "ok": true, "result": result})
		}
	}
	WriteAdminJSON(w, 200, map[string]any{"state": "PASS", "results": results})
}

func (admin *API) listMailBatches(w http.ResponseWriter, r *http.Request) {
	limit, offset, err := adminPage(r, 20, 100)
	if err != nil {
		WriteAdminError(w, 400, err.Error())
		return
	}
	status := r.URL.Query().Get("status")
	if status != "" && status != "unfinished" {
		WriteAdminError(w, 400, "未知批次状态")
		return
	}
	documents, total, err := admin.operations.storage.ListBatchDocuments(adminBatchPrefix, r.URL.Query().Get("q"), status == "unfinished", limit, offset)
	if err != nil {
		WriteAdminError(w, 500, err.Error())
		return
	}
	result := []map[string]any{}
	for _, doc := range documents {
		var batch AdminMailBatch
		if err := json.Unmarshal(doc.Payload, &batch); err != nil {
			WriteAdminError(w, 500, err.Error())
			return
		}
		result = append(result, map[string]any{"batch_id": batch.ID, "title": batch.Title, "created_utc": doc.UpdatedUTC, "recipients": len(batch.UserIDs), "all_players": batch.AllPlayers, "rewards": len(batch.Rewards), "done": doc.ReceiptCount})
	}
	WriteAdminJSON(w, 200, map[string]any{"state": "PASS", "batches": result, "total": total, "limit": limit, "offset": offset})
}
