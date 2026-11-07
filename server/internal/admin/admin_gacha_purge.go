package admin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"kairisei.local/server/internal/accountstore"
)

type gachaTrashEntry struct {
	GroupID  int    `json:"group_id"`
	Name     string `json:"name"`
	GachaIDs []int  `json:"gacha_ids"`
}

func (o *Operations) gachaTrash() []gachaTrashEntry {
	o.configMu.RLock()
	defer o.configMu.RUnlock()
	groups := map[int]*gachaTrashEntry{}
	for _, pool := range o.customGachas {
		if pool.Deleted {
			g := groups[pool.GroupID]
			if g == nil {
				g = &gachaTrashEntry{GroupID: pool.GroupID, Name: pool.Name}
				groups[pool.GroupID] = g
			}
			g.GachaIDs = append(g.GachaIDs, pool.GachaID)
		}
	}
	rows := make([]gachaTrashEntry, 0, len(groups))
	for _, g := range groups {
		slices.Sort(g.GachaIDs)
		for _, config := range o.gachaConfigurations {
			if config.Profile.GachaID == g.GroupID {
				g.Name = config.Profile.Name
				break
			}
		}
		rows = append(rows, *g)
	}
	slices.SortFunc(rows, func(a, b gachaTrashEntry) int { return a.GroupID - b.GroupID })
	return rows
}

// Caller has the configuration lock. Publication references and the permanent
// allocation cursor change in the same transaction as player counters.
func (o *Operations) gachaPurgePlan(groupID int) (accountstore.GachaPurgePlan, customGachaDocument, error) {
	plan := accountstore.GachaPurgePlan{GroupID: groupID}
	doc := o.customGachaDocument()
	for _, pool := range doc.Pools {
		if pool.GroupID == groupID {
			if !pool.Deleted {
				return plan, doc, errors.New("请先将卡池移入回收站")
			}
			plan.GachaIDs = append(plan.GachaIDs, pool.GachaID)
		}
	}
	if len(plan.GachaIDs) == 0 {
		return plan, doc, errors.New("回收站卡池不存在，或已被永久删除")
	}
	for _, pool := range doc.Pools {
		if pool.GroupID != groupID && slices.Contains(plan.GachaIDs, pool.TemplateID) {
			return plan, doc, fmt.Errorf("卡池 %d 仍引用此池作为模板，请先处理该配置", pool.GroupID)
		}
	}
	doc.Pools = slices.DeleteFunc(doc.Pools, func(p customGachaPool) bool { return p.GroupID == groupID })
	plan.Writes = append(plan.Writes, accountstore.DocumentWrite{Key: customGachaKey, Expected: o.customGachaRevision, Value: doc, Operation: "gacha-custom"})
	publication, err := o.storage.ReadDocument(gachaPublicationKey)
	if err != nil {
		return plan, doc, err
	}
	if publication.Revision > 0 {
		var policy gachaPublication
		if err = json.Unmarshal(publication.Payload, &policy); err != nil {
			return plan, doc, err
		}
		if slices.Contains(policy.GroupIDs, groupID) {
			policy.GroupIDs = slices.DeleteFunc(policy.GroupIDs, func(id int) bool { return id == groupID })
			policy.ExpectedRevision = nil
			plan.Writes = append(plan.Writes, accountstore.DocumentWrite{Key: gachaPublicationKey, Expected: publication.Revision, Value: policy, Operation: "gacha-policy"})
		}
	}
	return plan, doc, nil
}

func (a *API) purgeGacha(ctx context.Context, groupID int, expected string) (accountstore.OperationalCleanupReport, error) {
	o := a.operations
	o.configMu.RLock()
	plan, registry, err := o.gachaPurgePlan(groupID)
	o.configMu.RUnlock()
	if err != nil {
		return accountstore.OperationalCleanupReport{}, err
	}
	report, err := o.storage.PurgeGachaPool(ctx, plan, expected)
	if err != nil || !report.Applied {
		return report, err
	}
	o.configMu.Lock()
	defer o.configMu.Unlock()
	removed := map[int]bool{}
	for _, id := range plan.GachaIDs {
		removed[id] = true
		delete(o.gachaBases, id)
		delete(o.customGachas, id)
		delete(o.managedGachaGroupByID, id)
	}
	delete(o.managedGachaGroups, groupID)
	delete(o.defaultGachaPublication, groupID)
	kept := o.gachaConfigurations[:0]
	for _, config := range o.gachaConfigurations {
		if !removed[config.Profile.GachaID] {
			kept = append(kept, config)
		}
	}
	o.gachaConfigurations = kept
	o.customGachaRevision++
	o.customGachaNextID = registry.NextID
	o.gachaRevision++
	o.publishRuntimeLocked()
	return report, nil
}
