package cnbootstrap

import (
	"encoding/csv"
	"encoding/json"
	"net/http"
	"strconv"
)

// LOCAL_POLICY: installation versions are separate from catalog/CPK revisions.
// Publish this exact modern client version in the server list and login gate.
const cnMinimumClientVersion = "2.0.2"

const cnClientReleaseURL = "https://github.com/kuuhaku1314/kairisei-ma-ch/releases"

func cnClientVersionAllowed(version string) bool {
	// The server list publishes exact installation versions. A numerically
	// greater legacy track (such as 6.0.7) is not a compatible modern release.
	return version == cnMinimumClientVersion
}

func cnClientUpdateTips() string {
	return "请安装与本服务端配套的 " + cnMinimumClientVersion + " 客户端。更新内容和客户端下载均在 GitHub Releases；请覆盖安装以保留数据。"
}

func cnBootstrapServerList(host string, port int) http.HandlerFunc {
	return func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "text/plain; charset=utf-8")
		writer.Header().Set("Cache-Control", "no-store")
		rows := csv.NewWriter(writer)
		// CONFIRMED: ExServerItem matches explicit version strings before the
		// ALL fallback; Title.doStartLogin blocks when updateUrl is nonempty.
		// A later released APK must be added here when its server is published.
		_ = rows.Write([]string{"ALL", "0", "本地服务器", host, strconv.Itoa(port), "0", "", cnMinimumClientVersion, "", "", ""})
		_ = rows.Write([]string{"ALL", "0", "请更新客户端", host, strconv.Itoa(port), "0", "", "ALL", cnClientReleaseURL, cnClientUpdateTips(), ""})
		rows.Flush()
	}
}

func cnRejectOldClient(writer http.ResponseWriter) {
	writer.Header().Set("Content-Type", "application/json; charset=utf-8")
	writer.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(writer).Encode(map[string]any{
		"res_code": -1, "res_str": cnClientUpdateTips() + "\n" + cnClientReleaseURL,
		"update_url": cnClientReleaseURL,
	})
}
