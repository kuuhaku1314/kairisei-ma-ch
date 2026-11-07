package httpapi

import (
	"kairisei.local/server/internal/accounthttp"
	"kairisei.local/server/internal/game"
	"kairisei.local/server/internal/gamestate"
	"kairisei.local/server/internal/testfixture"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

type rejectedGift struct{}

func (rejectedGift) PrepareGiftCode(int, string, time.Time) (game.GiftCodeOffer, int, error) {
	return game.GiftCodeOffer{Batch: game.GiftCodeBatch{Title: "test", Rewards: []gamestate.Reward{{Type: 4, Num: 9}}}}, 0, nil
}
func (rejectedGift) CommitGiftCode(game.GiftCodeOffer, gamestate.State, []int64, time.Time) (int, error) {
	return 3, nil
}

func TestPositiveGiftFailureEvictsUncommittedAccount(t *testing.T) {
	durable := testfixture.RuntimeState(t)
	builds := 0
	router := accounthttp.New(accounthttp.Config{IdleLimit: 1, Build: func(int) (http.Handler, error) {
		builds++
		return New(Config{InitialState: durable, BaseURL: "http://127.0.0.1", GiftCodes: rejectedGift{}})
	}})
	defer router.InvalidateAll()
	for i := 0; i < 2; i++ {
		w := httptest.NewRecorder()
		request := httptest.NewRequest("POST", "/GiftCodeAd", strings.NewReader(`{"channel":"test","codenumber":"TEST-CODE"}`))
		request.Header.Set(accounthttp.AccountUserHeader, strconv.Itoa(durable.User.UserID))
		router.ServeHTTP(w, request)
		if w.Code != 200 || !strings.Contains(w.Body.String(), `"code":3`) {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	if builds != 2 {
		t.Fatal("positive gift error retained uncommitted mails", builds)
	}
}
