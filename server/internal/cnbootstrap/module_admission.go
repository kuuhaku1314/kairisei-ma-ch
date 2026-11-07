package cnbootstrap

import (
	"net/http"

	"kairisei.local/server/internal/protocol"
)

func (app *application) admitPVPStart(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if app.operations.ModuleSwitches()&protocol.ModulePVP != 0 {
			next(w, r)
			return
		}
		common := cnBootstrapCommon()
		common["res_code"] = -1200
		common["res_str"] = "竞技场当前未开放"
		writeCNProtocolResponseWithPopup(w, common, map[string]any{})
	}
}
