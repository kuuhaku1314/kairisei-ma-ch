package admin

import (
	"bytes"
	"errors"
	"net/http"
	"slices"

	"github.com/go-chi/chi/v5"

	"kairisei.local/server/internal/accountstore"
)

// Clearing the body keeps the revision monotonic so an old editor cannot
// recreate a discarded draft with a reused revision after delete/recreate.
func gachaDraftExists(doc accountstore.Document) bool {
	return doc.Revision > 0 && len(doc.Payload) > 0 && !bytes.Equal(bytes.TrimSpace(doc.Payload), []byte("null"))
}

// Variants retain independent contents and payment. The cover, schedule, odds message and
// cumulative draw limit are pool-wide; saves/publishes remain one atomic group operation.

type gachaGroupExpectation struct {
	Draft  int    `json:"draft_revision"`
	Live   int    `json:"live_revision"`
	SHA256 string `json:"sha256"`
}

// gachaGroupMembers lists the editable variants of a pool in ID order.
func (operations *Operations) gachaGroupMembers(groupID int) []int {
	members := []int{}
	for id, base := range operations.gachaBases {
		if groupID > 0 && base.GroupID == groupID {
			members = append(members, id)
		}
	}
	slices.Sort(members)
	return members
}

func (operations *Operations) gachaGroupShared(configs []AdminGachaConfig) error {
	first := configs[0]
	for _, config := range configs[1:] {
		if config.PlayCountMax != first.PlayCountMax {
			return errors.New("累计限抽次数由整池共享，各分池须使用同一上限")
		}
		if config.DailyPlayMax != first.DailyPlayMax {
			return errors.New("每日限抽次数由整池共享，各分池须使用同一上限")
		}
		if config.CategoryNum != first.CategoryNum {
			return errors.New("扭蛋分类由整池共享，各分池须使用同一分类")
		}
		if config.OddsMessage != first.OddsMessage {
			return errors.New("概率页说明由整池共享，各分池须使用同一说明")
		}
		if config.CoverPath != first.CoverPath {
			return errors.New("同一卡池的各抽法必须使用同一封面")
		}
		if config.StartUnix != first.StartUnix || config.EndUnix != first.EndUnix {
			return errors.New("开始与结束时间由整池共享，各分池须使用同一排期")
		}
	}
	return nil
}

// The native list has two draw buttons. Profession choices share a button,
// and alternative single-draw payments are resolved per player.
func (operations *Operations) validateGachaDrawButtons(configs []AdminGachaConfig) error {
	minByMax := make(map[int]int)
	maxByMin := make(map[int]int)
	multi := make(map[[2]int]bool)
	for _, config := range configs {
		if config.Closed {
			continue
		}
		base := operations.gachaBases[config.GachaID]
		applyGachaPayment(&base, config)
		maxDraw := base.CardNumMax
		if previous, ok := maxByMin[base.CardNum]; ok && previous != maxDraw {
			return errors.New("同一卡池不能同时开放最少抽数相同、最多抽数不同的抽法")
		}
		if previous, ok := minByMax[maxDraw]; ok && previous != base.CardNum {
			return errors.New("同一卡池不能同时开放最多抽数相同、最少抽数不同的抽法")
		}
		maxByMin[base.CardNum], minByMax[maxDraw] = maxDraw, base.CardNum
		if len(maxByMin) > maxGachaDrawCounts {
			return errors.New("游戏内每个卡池最多显示两种抽数，请关闭多余抽法")
		}
		if maxDraw > 1 {
			key := [2]int{maxDraw, int(base.ArthurType)}
			if multi[key] {
				return errors.New("同一职业的同一连抽按钮只能开放一种抽法")
			}
			multi[key] = true
		}
	}
	return nil
}

// gachaGroupAction saves drafts for, or publishes, every variant of one pool under a single lock: all
// revisions are checked and all variants validated before anything is written.
func (admin *API) gachaGroupAction(w http.ResponseWriter, r *http.Request) {
	if err := requireAdminMutation(r); err != nil {
		WriteAdminError(w, 403, err.Error())
		return
	}
	var body gachaGroupChange
	if err := DecodeAdminJSONLimit(r, &body, 8*1024*1024); err != nil {
		WriteAdminError(w, 400, err.Error())
		return
	}
	result, err := admin.operations.changeGachaGroup(body, chi.URLParam(r, "action"), admin.validateGachaConfig)
	if err != nil {
		var invalid gachaGroupInputError
		if errors.As(err, &invalid) {
			WriteAdminError(w, 400, err.Error())
		} else {
			writeContentError(w, err)
		}
		return
	}
	WriteAdminJSON(w, 200, result)
}

// applyPublishedGacha swaps one variant's live configuration while the caller holds configMu.
// No account cache or player save is bulk rewritten; accounts pick it up on their next request.
func (operations *Operations) applyPublishedGacha(config AdminGachaConfig) {
	configured := operations.configuredGacha(config.GachaID, config)
	for i := range operations.gachaConfigurations {
		if operations.gachaConfigurations[i].Profile.GachaID == config.GachaID {
			operations.gachaConfigurations[i] = configured
			operations.gachaRevision++
			return
		}
	}
	operations.gachaConfigurations = append(operations.gachaConfigurations, configured)
	operations.gachaRevision++
}
