package accountstore_test

import (
	"reflect"
	"testing"
	"time"

	"kairisei.local/server/internal/accountstore"
	"kairisei.local/server/internal/multiplayer"
	"kairisei.local/server/internal/testfixture"
)

func TestAwakeFinalCompletionPersistsAndReloads(t *testing.T) {
	accounts := testfixture.NewFriendCapacityTestAccounts(t)
	now := time.Now().Truncate(time.Second)
	expires := now.Add(time.Hour)
	completed := multiplayer.CompletedBattle{RoomID: 472, BossID: 1, OwnerMemberType: 1,
		EndType: 4, CompletedAtUnix: now.Unix(), OnlineUserIDs: []int{accountstore.PrimaryUserID},
		Members: make([]multiplayer.Member, 4)}
	for i := range completed.Members {
		completed.Members[i] = multiplayer.Member{MemberType: i + 1, ArthurType: i + 1}
	}
	completed.Members[0].UserID = accountstore.PrimaryUserID
	for range 2 {
		if err := accounts.SaveCompleted(completed, expires); err != nil {
			t.Fatal(err)
		}
	}
	got, until, err := accounts.LoadCompleted(completed.RoomID, now)
	if err != nil || !reflect.DeepEqual(got, completed) || !until.Equal(expires) {
		t.Fatalf("awake completion round trip: %+v until=%v err=%v", got, until, err)
	}
}
