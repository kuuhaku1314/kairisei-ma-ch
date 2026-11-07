package admin

import _ "embed"

// adminAccessJS drives 运营设置 › 登录与访问 and 游戏功能开关, and 账号管理 › 封禁登录.
//
//go:embed web/admin_access.js
var adminAccessJS []byte
