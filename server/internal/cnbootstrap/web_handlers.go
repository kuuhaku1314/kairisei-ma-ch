package cnbootstrap

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
)

var cn602IntroPlaceholderPNG = func() []byte {
	content, err := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=")
	if err != nil {
		panic("decode embedded CN intro placeholder: " + err.Error())
	}
	return content
}()

func cnBootstrapProducts(writer http.ResponseWriter, _ *http.Request) {
	writer.Header().Set("Content-Type", "application/json; charset=utf-8")
	writer.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(writer).Encode(map[string]any{
		"code":         200,
		"product_list": cnLocalShopProducts(),
	})
}

func cnBootstrapIntroPlaceholder(writer http.ResponseWriter, request *http.Request) {
	index, err := strconv.Atoi(chi.URLParam(request, "index"))
	if err != nil || index < 0 || index > 4 {
		http.NotFound(writer, request)
		return
	}
	writer.Header().Set("Content-Type", "image/png")
	writer.Header().Set("Cache-Control", "public, max-age=86400")
	_, _ = writer.Write(cn602IntroPlaceholderPNG)
}

func cnBootstrapDungeonSchedule(writer http.ResponseWriter, _ *http.Request) {
	writer.Header().Set("Content-Type", "text/html; charset=utf-8")
	writer.Header().Set("Cache-Control", "no-store")
	_, _ = io.WriteString(writer, `<!doctype html>
<html lang="zh-CN"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>副本日程表</title><style>
body{margin:0;background:#f2ead7;color:#392d1d;font-family:sans-serif}main{max-width:920px;margin:auto;padding:24px}
h1{margin:0 0 18px;color:#74531e;border-bottom:3px solid #c69b44;padding-bottom:12px}.status{background:#fff8df;border:2px solid #c69b44;border-radius:12px;padding:18px;line-height:1.7}
.grid{display:grid;grid-template-columns:repeat(3,1fr);gap:14px;margin-top:18px}.card{background:#fff;border:1px solid #b8934e;border-radius:10px;padding:16px}.card h2{margin:0 0 8px;color:#8a5c10;font-size:1.15rem}.open{color:#277234;font-weight:bold}
@media(max-width:700px){.grid{grid-template-columns:1fr}}
</style></head><body><main><h1>副本日程表</h1><section class="status">
<strong>本地离线副本开放规则</strong><br>活动副本不再受原运营时间窗限制，当前均为<span class="open">全天开放</span>；常驻普通副本仍按账号通关进度依次解锁。
</section><section class="grid">
<article class="card"><h2>普通活动副本</h2><span class="open">全天开放</span></article>
<article class="card"><h2>3D 活动副本</h2><span class="open">全天开放</span></article>
<article class="card"><h2>限时素材副本</h2><span class="open">全天开放</span></article>
</section></main></body></html>`)
}
