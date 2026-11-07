package admin

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"kairisei.local/server/internal/accountstore"
	"kairisei.local/server/internal/game"
	"kairisei.local/server/internal/operations"
)

const popupAnnouncementsKey = "popup-announcements"
const firstPopupAnnouncementID = 610000001
const maxPopupAnnouncements = 1000
const maxLivePopupAnnouncements = 20

type popupAnnouncementEntry struct {
	game.PopupAnnouncement
	Status string `json:"status"`
}

type popupAnnouncementRegistry struct {
	NextID int                      `json:"next_id"`
	Items  []popupAnnouncementEntry `json:"items"`
}

func validatePopupAnnouncement(p game.PopupAnnouncement, required bool) error {
	if utf8.RuneCountInString(p.Title) > 40 || utf8.RuneCountInString(p.Body) > 500 || utf8.RuneCountInString(p.ButtonText) > 12 {
		return errors.New("弹窗标题最多40字，正文最多500字，按钮最多12字")
	}
	if required && (strings.TrimSpace(p.Title) == "" || strings.TrimSpace(p.Body) == "" || strings.TrimSpace(p.ButtonText) == "") {
		return errors.New("发布前请填写标题、正文和按钮文字")
	}
	if p.StartUnix < 0 || p.EndUnix < 0 || p.EndUnix > 0 && p.StartUnix > 0 && p.EndUnix <= p.StartUnix {
		return errors.New("公告结束时间须晚于开始时间")
	}
	return nil
}

func (o *Operations) loadPopupAnnouncements() error {
	doc, err := o.storage.ReadDocument(popupAnnouncementsKey)
	if err != nil {
		return err
	}
	registry := popupAnnouncementRegistry{NextID: firstPopupAnnouncementID, Items: []popupAnnouncementEntry{}}
	if doc.Revision > 0 {
		if err := json.Unmarshal(doc.Payload, &registry); err != nil {
			return fmt.Errorf("decode popup announcements: %w", err)
		}
	}
	if registry.NextID < firstPopupAnnouncementID || registry.NextID > 2147483647 || registry.Items == nil || len(registry.Items) > maxPopupAnnouncements {
		return errors.New("invalid popup announcement registry")
	}
	seen := map[int]bool{}
	for _, p := range registry.Items {
		if p.ID < firstPopupAnnouncementID || p.ID >= registry.NextID || seen[p.ID] || p.PublishedAt < 0 ||
			(p.Status != "draft" && p.Status != "published" && p.Status != "stopped") ||
			(p.Status == "draft") != (p.PublishedAt == 0) || p.Enabled != (p.Status == "published") {
			return errors.New("invalid popup announcement identity or state")
		}
		if err := validatePopupAnnouncement(p.PopupAnnouncement, p.Status != "draft"); err != nil {
			return err
		}
		seen[p.ID] = true
	}
	o.popupAnnouncements, o.popupAnnouncementsRevision = registry, doc.Revision
	return nil
}

func (o *Operations) publishedPopupAnnouncements() []game.PopupAnnouncement {
	result := []game.PopupAnnouncement{}
	for _, p := range o.popupAnnouncements.Items {
		if p.PublishedAt > 0 {
			result = append(result, p.PopupAnnouncement)
		}
	}
	slices.SortFunc(result, func(a, b game.PopupAnnouncement) int {
		if a.PublishedAt != b.PublishedAt {
			if a.PublishedAt < b.PublishedAt {
				return -1
			}
			return 1
		}
		return a.ID - b.ID
	})
	return result
}

func (a *API) popupAnnouncements(w http.ResponseWriter, _ *http.Request) {
	o := a.operations
	o.configMu.RLock()
	items, revision := slices.Clone(o.popupAnnouncements.Items), o.popupAnnouncementsRevision
	o.configMu.RUnlock()
	if items == nil {
		items = []popupAnnouncementEntry{}
	}
	WriteAdminJSON(w, 200, map[string]any{"state": "PASS", "revision": revision, "items": items,
		"server_time": time.Now().Unix(),
		"limits":      map[string]int{"title": 40, "body": 500, "button_text": 12, "live": maxLivePopupAnnouncements, "total": maxPopupAnnouncements}})
}

func (a *API) popupAnnouncementAction(w http.ResponseWriter, r *http.Request) {
	if err := requireAdminMutation(r); err != nil {
		WriteAdminError(w, 403, err.Error())
		return
	}
	var body struct {
		Expected     *int   `json:"expected_revision"`
		Action       string `json:"action"`
		ID           int    `json:"id"`
		IDs          []int  `json:"ids"`
		Announcement *struct {
			Title      string `json:"title"`
			Body       string `json:"body"`
			ButtonText string `json:"button_text"`
			StartUnix  int64  `json:"start_unix"`
			EndUnix    int64  `json:"end_unix"`
		} `json:"announcement"`
	}
	if err := DecodeAdminJSONLimit(r, &body, 16*1024); err != nil || body.Expected == nil {
		WriteAdminError(w, 400, "请提交操作与配置版本")
		return
	}
	o := a.operations
	o.configMu.Lock()
	defer o.configMu.Unlock()
	if *body.Expected != o.popupAnnouncementsRevision {
		WriteAdminError(w, 409, "公告已被修改，请重新获取")
		return
	}
	registry := o.popupAnnouncements
	registry.Items = slices.Clone(registry.Items)
	index := slices.IndexFunc(registry.Items, func(p popupAnnouncementEntry) bool { return p.ID == body.ID })
	if body.ID != 0 && index < 0 {
		WriteAdminError(w, 404, "公告不存在")
		return
	}
	if body.Action == "cleanup" {
		if !o.MaintenanceState().Enabled {
			WriteAdminError(w, 403, "请先开启维护模式再清理公告")
			return
		}
		if body.ID != 0 || len(body.IDs) == 0 {
			WriteAdminError(w, 400, "请选择要清理的公告")
			return
		}
		remove := map[int]bool{}
		for _, id := range body.IDs {
			i := slices.IndexFunc(registry.Items, func(p popupAnnouncementEntry) bool { return p.ID == id })
			if i < 0 || remove[id] {
				WriteAdminError(w, 400, "清理公告不存在或重复")
				return
			}
			p := registry.Items[i]
			if p.Status == "published" && (p.EndUnix == 0 || p.EndUnix > time.Now().Unix()) {
				WriteAdminError(w, 400, "不能清理待生效或有效公告，请先停用")
				return
			}
			remove[id] = true
		}
		registry.Items = slices.DeleteFunc(registry.Items, func(p popupAnnouncementEntry) bool { return remove[p.ID] })
	} else if body.Action == "save" || body.Action == "publish" {
		if body.Announcement == nil || index >= 0 && registry.Items[index].Status != "draft" {
			WriteAdminError(w, 400, "只可编辑草稿；修改已发布公告请复制后重新发布")
			return
		}
		entry := popupAnnouncementEntry{Status: "draft", PopupAnnouncement: game.PopupAnnouncement{
			Title: strings.TrimSpace(body.Announcement.Title), Body: strings.TrimSpace(body.Announcement.Body),
			ButtonText: strings.TrimSpace(body.Announcement.ButtonText), StartUnix: body.Announcement.StartUnix, EndUnix: body.Announcement.EndUnix}}
		if entry.ButtonText == "" {
			entry.ButtonText = "确定"
		}
		if err := validatePopupAnnouncement(entry.PopupAnnouncement, body.Action == "publish"); err != nil {
			WriteAdminError(w, 400, err.Error())
			return
		}
		if index < 0 {
			if len(registry.Items) >= maxPopupAnnouncements || registry.NextID >= 2147483647 {
				WriteAdminError(w, 400, "公告数量达到上限")
				return
			}
			entry.ID = registry.NextID
			registry.NextID++
			registry.Items = append(registry.Items, entry)
			index = len(registry.Items) - 1
		} else {
			entry.ID = body.ID
		}
		if body.Action == "publish" {
			now := time.Now().Unix()
			if entry.EndUnix > 0 && entry.EndUnix <= now {
				WriteAdminError(w, 400, "不能发布已经结束的公告")
				return
			}
			live := 0
			for _, p := range registry.Items {
				if p.Status == "published" && (p.EndUnix == 0 || p.EndUnix > now) {
					live++
				}
			}
			if live >= maxLivePopupAnnouncements {
				WriteAdminError(w, 400, "请先停用旧公告，最多20条待生效或有效弹窗")
				return
			}
			entry.PublishedAt, entry.Enabled, entry.Status = now, true, "published"
		}
		registry.Items[index] = entry
	} else {
		if index < 0 {
			WriteAdminError(w, 400, "请指定公告")
			return
		}
		switch body.Action {
		case "stop":
			if registry.Items[index].Status != "published" {
				WriteAdminError(w, 400, "只能停用已发布公告")
				return
			}
			registry.Items[index].Status, registry.Items[index].Enabled = "stopped", false
		case "delete":
			if registry.Items[index].Status != "draft" {
				WriteAdminError(w, 400, "已发布公告只能停用，不能删除")
				return
			}
			registry.Items = slices.Delete(registry.Items, index, index+1)
		default:
			WriteAdminError(w, 400, "未知公告操作")
			return
		}
	}
	var doc accountstore.Document
	commit := func() error {
		var err error
		doc, err = o.writeDocument(popupAnnouncementsKey, *body.Expected, registry)
		if err != nil {
			return err
		}
		o.popupAnnouncements, o.popupAnnouncementsRevision = registry, doc.Revision
		o.publishRuntimeLocked()
		return nil
	}
	var err error
	if body.Action == "cleanup" {
		err = o.WithMaintenanceMutation(commit)
	} else {
		err = commit()
	}
	if err != nil {
		if errors.Is(err, operations.ErrMaintenanceConflict) {
			WriteAdminError(w, 409, err.Error())
		} else if errors.Is(err, accountstore.ErrDocumentConflict) {
			WriteAdminError(w, 409, "公告已被修改，请重新获取")
		} else {
			WriteAdminError(w, 500, err.Error())
		}
		return
	}
	id := body.ID
	if id == 0 && (body.Action == "save" || body.Action == "publish") {
		id = registry.Items[index].ID
	}
	WriteAdminJSON(w, 200, map[string]any{"state": "PASS", "revision": doc.Revision, "id": id})
}
