package cnbootstrap

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/netip"
	"strconv"

	"kairisei.local/server/internal/accountstore"
)

// Only the actual network peer is trusted. Forwarded headers are not a source
// of identity; IPv4-mapped peers share the same quota and attempt bucket.
func accountSource(r *http.Request) (netip.Addr, error) {
	peer, err := netip.ParseAddrPort(r.RemoteAddr)
	if err != nil {
		return netip.Addr{}, err
	}
	return peer.Addr().Unmap().WithZone(""), nil
}

func writeCreationRejection(w http.ResponseWriter, err error, login bool) bool {
	var challenge *accountstore.CreationChallengeRequired
	if errors.As(err, &challenge) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		response := map[string]any{"error_code": accountstore.CreationVerificationRequired, "verification": challenge.Verification}
		status := http.StatusPreconditionRequired
		if login {
			status = http.StatusOK
			response["res_code"], response["res_str"] = -12004, challenge.Error()
		} else {
			response["state"], response["error"] = "CHALLENGE", challenge.Error()
		}
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(response)
		return true
	}
	var rejection *accountstore.CreationRejection
	if !errors.As(err, &rejection) {
		return false
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	response := map[string]any{"error_code": rejection.Code}
	if rejection.RetryAfterSeconds > 0 {
		response["retry_after_seconds"] = rejection.RetryAfterSeconds
		w.Header().Set("Retry-After", strconv.Itoa(rejection.RetryAfterSeconds))
	}
	status := http.StatusTooManyRequests
	if login {
		code := -12003
		switch rejection.Code {
		case accountstore.CreationDisabled:
			code = -12001
		case accountstore.CreationDailyLimit:
			code = -12002
		}
		response["res_code"], response["res_str"] = code, rejection.Error()
		status = http.StatusOK
	} else {
		response["state"], response["error"] = "FAIL", rejection.Error()
		if rejection.Code == accountstore.CreationDisabled {
			status = http.StatusForbidden
		}
	}
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(response)
	return true
}
