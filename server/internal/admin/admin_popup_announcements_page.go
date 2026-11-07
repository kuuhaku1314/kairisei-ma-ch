package admin

import _ "embed"

// adminPopupAnnouncementsJS drives 公告与奖励 › 首页弹窗公告 and its 维护工具 cleanup.
//
//go:embed web/admin_popup_announcements.js
var adminPopupAnnouncementsJS []byte
