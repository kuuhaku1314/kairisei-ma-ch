package admin

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"kairisei.local/server/internal/accountstore"
)

func adminPage(r *http.Request, defaultLimit, maximum int) (int, int, error) {
	limit, offset := defaultLimit, 0
	for key, target := range map[string]*int{"limit": &limit, "offset": &offset} {
		if raw := r.URL.Query().Get(key); raw != "" {
			value, err := strconv.Atoi(raw)
			if err != nil {
				return 0, 0, fmt.Errorf("%s 无效", key)
			}
			*target = value
		}
	}
	if limit < 1 || limit > maximum || offset < 0 {
		return 0, 0, fmt.Errorf("分页须为1–%d项，偏移量不能为负", maximum)
	}
	return limit, offset, nil
}

func (admin *API) loadAccounts() ([]accountstore.AccountListItem, error) {
	rows, _, err := admin.accounts.QueryAccounts("", true, 0, 0)
	return rows, err
}

func (admin *API) resolveAccounts(w http.ResponseWriter, r *http.Request) {
	var body struct {
		UserIDs []int `json:"user_ids"`
	}
	if err := decodeAdminJSON(r, &body); err != nil || len(body.UserIDs) < 1 || len(body.UserIDs) > 500 {
		WriteAdminError(w, 400, "每次查询1–500名玩家")
		return
	}
	rows, _, err := admin.accounts.QueryAccounts("", false, 500, 0, body.UserIDs...)
	if err != nil {
		WriteAdminError(w, 500, err.Error())
		return
	}
	WriteAdminJSON(w, 200, map[string]any{"state": "PASS", "accounts": rows})
}

func (admin *API) accountList(w http.ResponseWriter, r *http.Request) {
	limit, offset, err := adminPage(r, 50, 200)
	if err != nil {
		WriteAdminError(w, 400, err.Error())
		return
	}
	q := r.URL.Query()
	filter := accountstore.AccountFilter{Search: q.Get("q"), IncludeSystem: q.Get("include_system") == "1", Binding: q.Get("binding"), Sort: q.Get("sort"), ActiveOnly: q.Get("active") == "1"}
	filter.LoginBlock = q.Get("login_block")
	if filter.LoginBlock != "" && filter.LoginBlock != "blocked" && filter.LoginBlock != "unblocked" {
		WriteAdminError(w, 400, "未知封禁状态")
		return
	}
	if filter.Binding != "" && filter.Binding != "bound" && filter.Binding != "guest" && filter.Binding != "system" {
		WriteAdminError(w, 400, "未知账号类型")
		return
	}
	if filter.Sort != "" && filter.Sort != "id" && filter.Sort != "created" && filter.Sort != "login" && filter.Sort != "level" {
		WriteAdminError(w, 400, "未知排序方式")
		return
	}
	if filter.Binding == "system" {
		filter.IncludeSystem = true
	}
	for key, spec := range map[string]struct {
		target   *int
		min, max int
	}{"level_min": {&filter.LevelMin, 1, 9999}, "level_max": {&filter.LevelMax, 1, 9999}, "arthur_type": {&filter.ArthurType, 1, 4}} {
		if raw := q.Get(key); raw != "" {
			value, err := strconv.Atoi(raw)
			if err != nil || value < spec.min || value > spec.max {
				WriteAdminError(w, 400, fmt.Sprintf("%s 须为%d–%d的整数", key, spec.min, spec.max))
				return
			}
			*spec.target = value
		}
	}
	if filter.LevelMin > 0 && filter.LevelMax > 0 && filter.LevelMin > filter.LevelMax {
		WriteAdminError(w, 400, "最低等级不能高于最高等级")
		return
	}
	for key, target := range map[string]*string{"created_after": &filter.CreatedAfter, "created_before": &filter.CreatedBefore, "login_after": &filter.LoginAfter, "login_before": &filter.LoginBefore} {
		if value := q.Get(key); value != "" {
			stamp, err := time.Parse(time.RFC3339, value)
			if err != nil {
				WriteAdminError(w, 400, "时间须为有效日期")
				return
			}
			*target = stamp.UTC().Format(time.RFC3339)
		}
	}
	if filter.CreatedAfter != "" && filter.CreatedBefore != "" && filter.CreatedAfter >= filter.CreatedBefore ||
		filter.LoginAfter != "" && filter.LoginBefore != "" && filter.LoginAfter >= filter.LoginBefore {
		WriteAdminError(w, 400, "开始时间须早于结束时间")
		return
	}
	if filter.ActiveOnly {
		filter.ActiveIDs = admin.activity().OnlineIDs
	}
	rows, total, err := admin.accounts.QueryFilteredAccounts(filter, limit, offset)
	if err != nil {
		WriteAdminError(w, 500, err.Error())
		return
	}
	WriteAdminJSON(w, 200, map[string]any{"state": "PASS", "accounts": rows, "total": total, "limit": limit, "offset": offset})
}
