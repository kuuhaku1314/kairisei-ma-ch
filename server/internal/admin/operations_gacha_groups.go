package admin

import (
	"encoding/json"
	"fmt"
	"slices"
	"strconv"

	"kairisei.local/server/internal/accountstore"
)

type gachaGroupChange struct {
	GroupID  int                           `json:"group_id"`
	Configs  []AdminGachaConfig            `json:"configs"`
	Expected map[int]gachaGroupExpectation `json:"expected"`
}

type gachaGroupInputError struct{ message string }

func (e gachaGroupInputError) Error() string { return e.message }

// The operation owns validation, atomic document commit and live publication.
// Its lock is released before the HTTP adapter encodes or writes a response.
func (o *Operations) changeGachaGroup(body gachaGroupChange, action string, validate func(AdminGachaConfig) (map[string]any, error)) (map[string]any, error) {
	publish, discard := action == "publish", action == "discard"
	o.configMu.Lock()
	defer o.configMu.Unlock()
	members := o.gachaGroupMembers(body.GroupID)
	if len(members) == 0 {
		return nil, gachaGroupInputError{"不能编辑新手保留卡池或未知卡池"}
	}
	configs := make([]AdminGachaConfig, len(members))
	drafts := make(map[int]accountstore.Document, len(members))
	if !publish && !discard && len(body.Configs) != len(members) {
		return nil, gachaGroupInputError{"请一次提交此卡池的全部抽法"}
	}
	for i, id := range members {
		if pool, custom := o.customGachas[id]; custom && pool.Deleted {
			return nil, gachaGroupInputError{"卡池在回收站中，请先恢复后编辑"}
		}
		key := strconv.Itoa(id)
		draft, err := o.storage.ReadDocument("gacha-draft:" + key)
		if err != nil {
			return nil, err
		}
		expected, listed := body.Expected[id]
		if !listed || draft.Revision != expected.Draft {
			return nil, accountstore.ErrDocumentConflict
		}
		drafts[id] = draft
		if !publish && !discard {
			if body.Configs[i].GachaID != id {
				return nil, gachaGroupInputError{"请按卡池抽法顺序提交全部抽法"}
			}
			configs[i] = body.Configs[i]
			continue
		}
		live, err := o.storage.ReadDocument("gacha-live:" + key)
		if err != nil {
			return nil, err
		}
		if live.Revision != expected.Live || (publish && (!gachaDraftExists(draft) || draft.SHA256 != expected.SHA256)) {
			return nil, accountstore.ErrDocumentConflict
		}
		if discard {
			continue
		}
		if err := json.Unmarshal(draft.Payload, &configs[i]); err != nil {
			return nil, err
		}
	}
	if discard {
		writes := make([]accountstore.DocumentWrite, len(members))
		for i, id := range members {
			writes[i] = accountstore.DocumentWrite{Key: "gacha-draft:" + strconv.Itoa(id), Expected: drafts[id].Revision, Value: nil, Operation: "gacha-draft-discard"}
		}
		docs, err := o.storage.WriteDocuments(writes)
		if err != nil {
			return nil, err
		}
		result := map[int]accountstore.Document{}
		for i, id := range members {
			result[id] = docs[i]
		}
		return map[string]any{"state": "PASS", "documents": result}, nil
	}
	if err := o.gachaGroupShared(configs); err != nil {
		return nil, gachaGroupInputError{err.Error()}
	}
	if !slices.ContainsFunc(configs, func(config AdminGachaConfig) bool { return !config.Closed }) {
		return nil, gachaGroupInputError{"至少开放一种抽法；要关闭整个卡池请在「扭蛋发布」操作"}
	}
	if err := o.validateGachaDrawButtons(configs); err != nil {
		return nil, gachaGroupInputError{err.Error()}
	}
	previews := make(map[int]map[string]any, len(members))
	for i, config := range configs {
		preview, err := validate(config)
		if err != nil {
			return nil, gachaGroupInputError{fmt.Sprintf("抽法 %d：%v", members[i], err)}
		}
		if publish && !preview["publishable"].(bool) {
			return nil, gachaGroupInputError{"发布被资源闭包阻止，详见预览中的卡牌 ID"}
		}
		previews[members[i]] = preview
	}
	writes := make([]accountstore.DocumentWrite, len(members))
	for i, config := range configs {
		id := members[i]
		key, expected := "gacha-draft:"+strconv.Itoa(id), drafts[id].Revision
		if publish {
			key, expected = "gacha-live:"+strconv.Itoa(id), body.Expected[id].Live
		}
		operation := "gacha-draft"
		if publish {
			operation = "gacha-live"
		}
		writes[i] = accountstore.DocumentWrite{Key: key, Expected: expected, Value: config, Operation: operation}
	}
	if publish {
		for _, id := range members {
			writes = append(writes, accountstore.DocumentWrite{Key: "gacha-draft:" + strconv.Itoa(id), Expected: drafts[id].Revision, Value: nil, Operation: "gacha-draft-consumed"})
		}
	}
	committed, err := o.storage.WriteDocuments(writes)
	if err != nil {
		return nil, err
	}
	documents := make(map[int]accountstore.Document, len(members))
	clearedDrafts := map[int]accountstore.Document{}
	for i, config := range configs {
		if publish {
			o.applyPublishedGacha(config)
			clearedDrafts[members[i]] = committed[len(members)+i]
		}
		documents[members[i]] = committed[i]
	}
	if publish {
		o.publishRuntimeLocked()
	}
	return map[string]any{"state": "PASS", "documents": documents, "drafts": clearedDrafts, "previews": previews}, nil
}
