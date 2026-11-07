package cnbootstrap

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"kairisei.local/server/internal/accountstore"
	"kairisei.local/server/internal/testfixture"
)

func TestClientCreationProtocolUsesPeerAndShortRefusals(t *testing.T) {
	accounts := testfixture.NewFriendCapacityTestAccounts(t)
	var limit atomic.Int64
	limit.Store(1)
	if err := accounts.AttachCreationPolicy(func() accountstore.CreationPolicy {
		return accountstore.CreationPolicy{DailyLimit: int(limit.Load()), VerificationBits: 0}
	}); err != nil {
		t.Fatal(err)
	}
	login := localClientLogin(accounts, "http://127.0.0.1", "cpk", "image", func(localLoginPayload) (string, string, error) { return "patch", "version", nil })
	gateway := newCNAccountGateway(accounts)
	call := func(bind bool, number int, peer string) (*httptest.ResponseRecorder, map[string]json.RawMessage) {
		t.Helper()
		uuid := fmt.Sprintf("f1610000-0000-4000-8000-%012d", number)
		body := fmt.Sprintf(`{"uuid":%q,"clver":%q}`, uuid, cnMinimumClientVersion)
		path := "/local/client/login"
		if bind {
			path = "/local/account/bind"
			body = fmt.Sprintf(`{"uuid":%q,"username":"player_%d","password":"valid-password"}`, uuid, number)
		}
		r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
		r.RemoteAddr = peer
		r.Header.Set("X-Kairisei-Account", "1")
		r.Header.Set("X-Forwarded-For", fmt.Sprintf("203.0.113.%d", number))
		r.Header.Set("X-Real-IP", "203.0.113.200")
		r.Header.Set("CF-Connecting-IP", "203.0.113.201")
		w := httptest.NewRecorder()
		if bind {
			gateway.ServeHTTP(w, r)
		} else {
			login(w, r)
		}
		var result map[string]json.RawMessage
		if w.Code != 400 {
			if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
				t.Fatal("invalid JSON", w.Body.String(), err)
			}
		}
		return w, result
	}
	w, result := call(false, 1, "[::ffff:192.0.2.1]:1234")
	if w.Code != 200 || string(result["res_code"]) != "0" {
		t.Fatal("initial mapped-peer login failed", w.Code, w.Body.String())
	}
	w, result = call(false, 2, "192.0.2.1:4567")
	if w.Code != 200 || string(result["res_code"]) != "-12002" || string(result["error_code"]) != `"account_creation_daily_limit"` {
		t.Fatal("quota login refusal", w.Code, w.Body.String())
	}
	if len(result) != 4 || result["userid"] != nil || result["sess_key"] != nil || w.Header().Get("Retry-After") == "" {
		t.Fatal("short refusal contained success fields or omitted retry", w.Body.String())
	}
	w, result = call(true, 3, "192.0.2.1:4567")
	if w.Code != 429 || string(result["state"]) != `"FAIL"` || string(result["error_code"]) != `"account_creation_daily_limit"` || w.Header().Get("Retry-After") == "" {
		t.Fatal("binding quota refusal", w.Code, w.Body.String())
	}
	w, _ = call(true, 4, "192.0.2.2:4567")
	if w.Code != 200 {
		t.Fatal("other peer blocked", w.Body.String())
	}
	limit.Store(0)
	w, result = call(false, 5, "192.0.2.3:1234")
	if w.Code != 200 || string(result["res_code"]) != "-12001" || result["retry_after_seconds"] != nil || w.Header().Get("Retry-After") != "" {
		t.Fatal("disabled login refusal", w.Body.String())
	}
	w, result = call(true, 6, "192.0.2.3:1234")
	if w.Code != 403 || string(result["error_code"]) != `"account_creation_disabled"` || result["retry_after_seconds"] != nil {
		t.Fatal("disabled binding refusal", w.Code, w.Body.String())
	}
	w, result = call(false, 1, "192.0.2.1:1234")
	if w.Code != 200 || string(result["res_code"]) != "0" {
		t.Fatal("existing identity hit disabled policy", w.Body.String())
	}
	for _, bind := range []bool{false, true} {
		w, _ := call(bind, 7, "")
		if w.Code != 400 {
			t.Fatal("invalid peer accepted", w.Code)
		}
	}
	for index := range 13 {
		w, result = call(false, index+100, "192.0.2.10:1234")
		want := "-12001"
		if index == 12 {
			want = "-12003"
		}
		if w.Code != 200 || string(result["res_code"]) != want {
			t.Fatal("creation rate result", w.Body.String())
		}
	}
	w, result = call(true, 200, "192.0.2.10:1234")
	if w.Code != 429 || string(result["error_code"]) != `"account_creation_rate_limited"` || w.Header().Get("Retry-After") == "" {
		t.Fatal("shared creation attempt bucket not used", w.Code, w.Body.String())
	}
	db, _ := accounts.Database().OpenRead()
	var peers, count int
	if err := db.QueryRow(`SELECT COUNT(*),SUM(created_count) FROM cn_account_creation_daily`).Scan(&peers, &count); err != nil || peers != 2 || count != 2 {
		t.Fatal("spoof headers or mapped peer split counts", peers, count, err)
	}
	if accounts.CreationRejections()[accountstore.CreationRateLimited] != 2 {
		t.Fatal("HTTP refusals not aggregated")
	}
}

func solveCreationChallenge(t *testing.T, challenge accountstore.CreationChallenge) accountstore.CreationProof {
	t.Helper()
	if challenge.Algorithm != "sha256-prefix-v1" || len(challenge.Challenge) != 100 || challenge.Difficulty != 18 && challenge.Difficulty != 20 {
		t.Fatal("unsupported challenge", challenge)
	}
	var input [117]byte
	copy(input[:100], challenge.Challenge)
	input[100] = ':'
	var counter [8]byte
	for candidate := uint64(0); candidate < 1<<24; candidate++ {
		binary.BigEndian.PutUint64(counter[:], candidate)
		hex.Encode(input[101:], counter[:])
		digest := sha256.Sum256(input[:])
		if digest[0] == 0 && digest[1] == 0 && digest[2]>>(24-challenge.Difficulty) == 0 {
			return accountstore.CreationProof{Challenge: challenge.Challenge, Solution: string(input[101:])}
		}
	}
	t.Fatal("bounded test solver exhausted")
	return accountstore.CreationProof{}
}

func TestClientCreationPowChallengeRetryAndSlotRelease(t *testing.T) {
	accounts := testfixture.NewFriendCapacityTestAccounts(t)
	if err := accounts.AttachCreationPolicy(func() accountstore.CreationPolicy {
		return accountstore.CreationPolicy{DailyLimit: 10, VerificationBits: 18}
	}); err != nil {
		t.Fatal(err)
	}
	login := localClientLogin(accounts, "http://127.0.0.1", "cpk", "image", func(localLoginPayload) (string, string, error) { return "patch", "version", nil })
	gateway := newCNAccountGateway(accounts)
	call := func(bind bool, number int, peer string, proof any) (*httptest.ResponseRecorder, map[string]json.RawMessage) {
		t.Helper()
		payload := map[string]any{"uuid": fmt.Sprintf("f1620000-0000-4000-8000-%012d", number)}
		path := "/local/client/login"
		if bind {
			path = "/local/account/bind"
			payload["username"] = fmt.Sprintf("pow_%d", number)
			payload["password"] = "valid-password"
		} else {
			payload["clver"] = cnMinimumClientVersion
		}
		if proof != nil {
			payload["creation_proof"] = proof
		}
		body, err := json.Marshal(payload)
		if err != nil {
			t.Fatal(err)
		}
		r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(string(body)))
		r.RemoteAddr = peer
		r.Header.Set("X-Kairisei-Account", "1")
		w := httptest.NewRecorder()
		if bind {
			gateway.ServeHTTP(w, r)
		} else {
			login(w, r)
		}
		var result map[string]json.RawMessage
		if w.Code != 400 {
			if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
				t.Fatal("invalid reply", w.Code, w.Body.String(), err)
			}
		}
		if len(gateway.workers) != 0 {
			t.Fatal("password worker slot retained")
		}
		return w, result
	}
	parseChallenge := func(w *httptest.ResponseRecorder, result map[string]json.RawMessage, bind bool) accountstore.CreationChallenge {
		t.Helper()
		if string(result["error_code"]) != `"account_creation_verification_required"` || result["retry_after_seconds"] != nil || w.Header().Get("Retry-After") != "" {
			t.Fatal("challenge failure shape", w.Code, w.Body.String())
		}
		if bind {
			if w.Code != 428 || string(result["state"]) != `"CHALLENGE"` {
				t.Fatal("binding challenge", w.Code, w.Body.String())
			}
		} else if w.Code != 200 || string(result["res_code"]) != "-12004" || result["userid"] != nil || result["sess_key"] != nil {
			t.Fatal("SDK challenge", w.Code, w.Body.String())
		}
		var challenge accountstore.CreationChallenge
		if err := json.Unmarshal(result["verification"], &challenge); err != nil {
			t.Fatal(err)
		}
		return challenge
	}
	// More than eight SDK challenges and four binding challenges must all finish
	// without retaining their original global worker slots.
	for index := range 10 {
		for _, bind := range []bool{false, true} {
			w, result := call(bind, index+10, fmt.Sprintf("192.0.2.%d:1234", index+10), nil)
			parseChallenge(w, result, bind)
		}
	}
	db, _ := accounts.Database().OpenRead()
	var count int
	if err := db.QueryRow(`SELECT COALESCE(SUM(created_count),0) FROM cn_account_creation_daily`).Scan(&count); err != nil || count != 0 {
		t.Fatal("challenge requests created accounts", count, err)
	}
	// A challenge issued by either entry can create the same UUID via the other;
	// it is not tied to the network used while solving.
	w, result := call(false, 1, "192.0.2.91:1234", nil)
	proof := solveCreationChallenge(t, parseChallenge(w, result, false))
	w, _ = call(true, 1, "192.0.2.92:1234", proof)
	if w.Code != 200 {
		t.Fatal("SDK proof could not bind", w.Code, w.Body.String())
	}
	w, result = call(true, 2, "192.0.2.94:1234", nil)
	proof = solveCreationChallenge(t, parseChallenge(w, result, true))
	w, result = call(false, 2, "192.0.2.93:1234", proof)
	if w.Code != 200 || string(result["res_code"]) != "0" {
		t.Fatal("binding proof could not log in", w.Code, w.Body.String())
	}
	w, result = call(false, 1, "192.0.2.92:1234", accountstore.CreationProof{Challenge: "invalid", Solution: "INVALID"})
	if w.Code != 200 || string(result["res_code"]) != "0" {
		t.Fatal("committed identity required another proof", w.Body.String())
	}
	for _, bad := range []any{map[string]any{"challenge": strings.Repeat("A", 101)}, map[string]any{"solution": 1}, "bad-proof", json.RawMessage(`null`)} {
		for _, bind := range []bool{false, true} {
			w, _ := call(bind, 3, "192.0.2.95:1234", bad)
			if w.Code != 400 {
				t.Fatal("invalid proof shape accepted", w.Code)
			}
		}
	}
	for index := range 13 {
		w, result = call(false, index+100, "192.0.2.96:1234", nil)
		if index < 12 {
			parseChallenge(w, result, false)
		} else if string(result["res_code"]) != "-12003" {
			t.Fatal("proof requests bypassed creation attempts", w.Body.String())
		}
	}
	w, result = call(true, 200, "192.0.2.96:1234", nil)
	if w.Code != 429 || string(result["error_code"]) != `"account_creation_rate_limited"` {
		t.Fatal("cross-entry attempt bucket", w.Code, w.Body.String())
	}
	if err := accounts.Database().Close(); err != nil {
		t.Fatal(err)
	}
	w, _ = call(false, 300, "192.0.2.97:1234", nil)
	if w.Code != 400 || strings.Contains(w.Body.String(), "verification_required") {
		t.Fatal("storage error disguised as challenge", w.Code, w.Body.String())
	}
}
