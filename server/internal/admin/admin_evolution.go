package admin

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"

	"kairisei.local/server/internal/game"
)

const evolutionPolicyKey = "card-evolution-policy"

type evolutionPolicyDocument struct {
	Closed []game.EvolutionPath `json:"closed_paths"`
}

func (o *Operations) validateEvolutionPaths(paths []game.EvolutionPath) error {
	if len(paths) > len(o.evolutionEdges) {
		return errors.New("关闭路线数量超出进化目录")
	}
	seen := map[game.EvolutionPath]bool{}
	for _, edge := range paths {
		if _, ok := o.evolutionEdges[edge]; !ok || seen[edge] {
			return fmt.Errorf("未知或重复进化路线 %d → %d", edge.FromCardID, edge.ToCardID)
		}
		seen[edge] = true
	}
	return nil
}

func (o *Operations) loadEvolutionRestrictions() error {
	doc, err := o.storage.ReadDocument(evolutionPolicyKey)
	if err != nil {
		return err
	}
	policy := evolutionPolicyDocument{Closed: []game.EvolutionPath{}}
	if doc.Revision > 0 {
		if err = json.Unmarshal(doc.Payload, &policy); err != nil {
			return err
		}
	}
	if err = o.validateEvolutionPaths(policy.Closed); err != nil {
		return err
	}
	o.evolutionClosed, o.evolutionRevision = policy.Closed, doc.Revision
	o.evolutionPolicy = game.NewEvolutionRestrictions(policy.Closed)
	return nil
}

func (a *API) evolutionEditor(w http.ResponseWriter, _ *http.Request) {
	o := a.operations
	o.configMu.RLock()
	defer o.configMu.RUnlock()
	type node struct {
		ID         int    `json:"card_id"`
		Name       string `json:"name"`
		Image      string `json:"image_url"`
		Rarity     int    `json:"rarity"`
		Job        int8   `json:"arthur_type"`
		LimitCount int    `json:"limit_count"`
	}
	type edge struct {
		game.EvolutionPath
		Type int `json:"type"`
	}
	ids := map[int]bool{}
	edges := make([]edge, 0, len(o.evolutionEdges))
	for path, kind := range o.evolutionEdges {
		edges = append(edges, edge{path, kind})
		ids[path.FromCardID], ids[path.ToCardID] = true, true
	}
	slices.SortFunc(edges, func(a, b edge) int {
		if a.FromCardID != b.FromCardID {
			return a.FromCardID - b.FromCardID
		}
		return a.ToCardID - b.ToCardID
	})
	nodes := make([]node, 0, len(ids))
	for id := range ids {
		card, ok := a.catalogByKey[adminCatalogKey(6, id)]
		if !ok {
			WriteAdminError(w, 503, fmt.Sprintf("进化目录卡牌 %d 缺少名称／头像信息", id))
			return
		}
		name, limit := card.Name, 0
		if card.Combat != nil {
			limit = card.Combat.LimitCount
			if card.Combat.Prefix != "" {
				name = "【" + card.Combat.Prefix + "】" + name
			}
		}
		nodes = append(nodes, node{id, name, card.ImageURL, card.Rarity, card.ArthurType, limit})
	}
	slices.SortFunc(nodes, func(a, b node) int { return a.ID - b.ID })
	WriteAdminJSON(w, 200, map[string]any{"state": "PASS", "nodes": nodes, "edges": edges, "closed_paths": o.evolutionClosed, "revision": o.evolutionRevision})
}

func (a *API) saveEvolutionPolicy(w http.ResponseWriter, r *http.Request) {
	if err := requireAdminMutation(r); err != nil {
		WriteAdminError(w, 403, err.Error())
		return
	}
	var body struct {
		evolutionPolicyDocument
		Expected *int `json:"expected_revision"`
	}
	if DecodeAdminJSONLimit(r, &body, 2*1024*1024) != nil || body.Expected == nil || body.Closed == nil {
		WriteAdminError(w, 400, "请提交关闭路线和当前配置版本")
		return
	}
	o := a.operations
	o.configMu.Lock()
	defer o.configMu.Unlock()
	if *body.Expected != o.evolutionRevision {
		WriteAdminError(w, 409, "配置已变化，请刷新后重新核对")
		return
	}
	if err := o.validateEvolutionPaths(body.Closed); err != nil {
		WriteAdminError(w, 400, err.Error())
		return
	}
	slices.SortFunc(body.Closed, func(a, b game.EvolutionPath) int {
		if a.FromCardID != b.FromCardID {
			return a.FromCardID - b.FromCardID
		}
		return a.ToCardID - b.ToCardID
	})
	doc, err := o.writeDocument(evolutionPolicyKey, *body.Expected, body.evolutionPolicyDocument)
	if err != nil {
		writeContentError(w, err)
		return
	}
	o.evolutionClosed, o.evolutionRevision = body.Closed, doc.Revision
	o.evolutionPolicy = game.NewEvolutionRestrictions(body.Closed)
	o.publishRuntimeLocked()
	WriteAdminJSON(w, 200, map[string]any{"state": "PASS", "closed_paths": body.Closed, "revision": doc.Revision})
}
