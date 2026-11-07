package admin

import _ "embed"

// adminRuntimeLogsJS drives 系统 › 运行日志 over GET /api/runtime-logs.
//
//go:embed web/admin_runtime_logs.js
var adminRuntimeLogsJS []byte
