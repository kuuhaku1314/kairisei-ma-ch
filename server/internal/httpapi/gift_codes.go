package httpapi

import (
	"net/http"
	"time"
)

func (a *API) giftCodeAd(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Channel string `json:"channel"`
		Code    string `json:"codenumber"`
	}
	if err := decodeExact(r, []string{"channel", "codenumber"}, &input); err != nil {
		a.writeStoreError(w, err)
		return
	}
	// All non-committed outcomes discard this mutable account candidate,
	// including positive business codes whose Common result is otherwise zero.
	a.requestRejected = true
	if a.giftCodes == nil {
		a.writeProtocol(w, map[string]int{"code": 1})
		return
	}
	now := time.Now()
	offer, code, err := a.giftCodes.PrepareGiftCode(a.initialState.User.UserID, input.Code, now)
	if err != nil {
		a.logger.Error("read gift code policy", "error", err)
		writeError(w, 500, "读取兑换配置失败，请重试")
		return
	}
	if code != 0 {
		a.writeProtocol(w, map[string]int{"code": code})
		return
	}
	ids, err := a.account.PrepareGiftPresents(offer.Batch)
	if err != nil {
		writeError(w, 500, "兑换奖励配置无效")
		return
	}
	code, err = a.giftCodes.CommitGiftCode(offer, a.account.Snapshot(a.initialState), ids, time.Now())
	if err != nil {
		a.logger.Error("commit gift redemption", "code_id", offer.CodeID, "error", err)
		writeError(w, 500, "兑换保存失败，请重试")
		return
	}
	if code != 0 {
		a.writeProtocol(w, map[string]int{"code": code})
		return
	}
	a.requestRejected = false
	a.writeProtocol(w, map[string]int{"code": 0})
}
