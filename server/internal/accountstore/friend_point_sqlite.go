package accountstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"kairisei.local/server/internal/game"
)

func (accounts *Accounts) ListFollowedFriendPointAccounts(userID int) ([]game.FriendPointAccountRelation, error) {
	const selection = `WITH selected(user_id) AS (
  SELECT followed_user_id FROM cn_local_account_follow WHERE follower_user_id = ?1
 ) `
	return accounts.queryPartnerRelations(userID, selection+partnerRelationProjection+" ORDER BY a.user_id", userID)
}

// The client displays a recent window and the full count, without next-page
// requests. Never decode projections outside that window or trim relationships.
func (accounts *Accounts) ListFollowerFriendPointAccounts(userID int, limit int) ([]game.FriendPointAccountRelation, int, error) {
	if userID < PrimaryUserID || limit <= 0 {
		return nil, 0, errors.New("invalid CN follower display selection")
	}
	database, err := accounts.storage.OpenRead()
	if err != nil {
		return nil, 0, err
	}
	var total int
	if err := database.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM cn_local_account_follow WHERE followed_user_id = ?`, userID).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count CN followers: %w", err)
	}
	const selection = `WITH selected(user_id, followed_at) AS (
  SELECT follower_user_id, julianday(created_utc) FROM cn_local_account_follow
  WHERE followed_user_id = ?1
  ORDER BY julianday(created_utc) DESC, follower_user_id DESC LIMIT ?2
 ) `
	relations, err := accounts.queryPartnerRelations(userID, selection+partnerRelationProjection+
		" ORDER BY s.followed_at DESC, a.user_id DESC", userID, limit)
	return relations, total, err
}

func (accounts *Accounts) FindFriendPointAccountByInviteID(userID int, inviteID string) ([]game.FriendPointAccountRelation, error) {
	target, err := strconv.Atoi(inviteID)
	if err != nil {
		return nil, nil
	}
	if target > 900000000 && target <= 900000000+systemPartnerArthurCount {
		target = SystemPartnerUserIDBase + target - 900000000
	}
	if InviteID(target) != inviteID {
		return nil, nil
	}
	return accounts.LoadFriendPointAccountRelations(userID, []int{target})
}

// Battle/room callers already know their target accounts. Check only those
// indexed relationship pairs, regardless of how many other followers exist.
func (accounts *Accounts) FriendPointAccountStates(userID int, targetUserIDs []int) (map[int]int8, error) {
	if userID < PrimaryUserID {
		return nil, errors.New("invalid CN local-account relationship owner")
	}
	states := make(map[int]int8, len(targetUserIDs))
	arguments := []any{userID}
	values := make([]string, 0, len(targetUserIDs))
	for _, target := range targetUserIDs {
		if target < PrimaryUserID {
			return nil, errors.New("invalid CN local-account relationship target")
		}
		if target == userID {
			continue
		}
		if _, duplicate := states[target]; duplicate {
			continue
		}
		states[target] = game.FriendStateOther
		arguments = append(arguments, target)
		values = append(values, fmt.Sprintf("(?%d)", len(arguments)))
	}
	if len(values) == 0 {
		return states, nil
	}
	database, err := accounts.storage.OpenRead()
	if err != nil {
		return nil, err
	}
	query := `WITH selected(user_id) AS (VALUES ` + strings.Join(values, ",") + `)
  SELECT s.user_id,
  EXISTS (SELECT 1 FROM cn_local_account_follow f WHERE f.follower_user_id = ?1 AND f.followed_user_id = s.user_id),
  EXISTS (SELECT 1 FROM cn_local_account_follow f WHERE f.follower_user_id = s.user_id AND f.followed_user_id = ?1)
  FROM selected s`
	rows, err := database.QueryContext(context.Background(), query, arguments...)
	if err != nil {
		return nil, fmt.Errorf("query targeted CN relationships: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var target int
		var follows, followedBy bool
		if err := rows.Scan(&target, &follows, &followedBy); err != nil {
			return nil, fmt.Errorf("scan targeted CN relationship: %w", err)
		}
		switch {
		case follows && followedBy:
			states[target] = game.FriendStateFriend
		case follows:
			states[target] = game.FriendStateFollow
		case followedBy:
			states[target] = game.FriendStateFollower
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate targeted CN relationships: %w", err)
	}
	return states, nil
}

func (accounts *Accounts) FollowFriendPointAccounts(userID int, targetUserIDs []int, followMaximum int) (game.FollowAddResult, error) {
	if userID < PrimaryUserID || followMaximum <= 0 {
		return game.FollowAddResult{}, errors.New("invalid CN local-account follow selection")
	}
	if len(targetUserIDs) == 0 {
		return game.FollowAddResult{RequestUserIDs: []int{}}, nil
	}
	available, err := accounts.LoadFriendPointAccountRelations(userID, targetUserIDs)
	if err != nil {
		return game.FollowAddResult{}, err
	}
	validTargets := make(map[int]bool, len(available))
	for _, relation := range available {
		if strings.TrimSpace(relation.State.User.Name) != "" {
			validTargets[relation.State.User.UserID] = true
		}
	}
	seen := make(map[int]bool, len(targetUserIDs))
	selected := make([]int, 0, len(targetUserIDs))
	for _, target := range targetUserIDs {
		if target == userID {
			return game.FollowAddResult{}, &game.FollowAddError{ResultCode: -600, ResultString: "不能关注自己。"}
		}
		if !validTargets[target] {
			return game.FollowAddResult{}, &game.FollowAddError{ResultCode: -601, ResultString: "该玩家暂不可关注，请刷新列表后重试。"}
		}
		if !seen[target] {
			seen[target] = true
			selected = append(selected, target)
		}
	}
	accounts.mu.Lock()
	defer accounts.mu.Unlock()
	database, err := accounts.storage.Open()
	if err != nil {
		return game.FollowAddResult{}, err
	}
	transaction, err := database.BeginTx(context.Background(), nil)
	if err != nil {
		return game.FollowAddResult{}, fmt.Errorf("begin CN local-account follow: %w", err)
	}
	defer transaction.Rollback()
	var count int
	if err := transaction.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM cn_local_account_follow WHERE follower_user_id = ?`, userID).Scan(&count); err != nil {
		return game.FollowAddResult{}, fmt.Errorf("count CN local-account follows: %w", err)
	}
	if count+len(selected) > followMaximum {
		return game.FollowAddResult{}, &game.FollowAddError{ResultCode: -3410, ResultString: "关注人数已达上限。"}
	}
	for _, target := range selected {
		var exists bool
		if err := transaction.QueryRowContext(context.Background(), `SELECT EXISTS (SELECT 1 FROM cn_local_account_follow WHERE follower_user_id = ? AND followed_user_id = ?)`, userID, target).Scan(&exists); err != nil {
			return game.FollowAddResult{}, fmt.Errorf("check CN local-account follow: %w", err)
		}
		if exists {
			return game.FollowAddResult{}, &game.FollowAddError{ResultCode: -3411, ResultString: "已经关注该玩家。"}
		}
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	for _, target := range selected {
		if _, err := transaction.ExecContext(context.Background(), `INSERT INTO cn_local_account_follow (follower_user_id, followed_user_id, created_utc) VALUES (?, ?, ?)`, userID, target, now); err != nil {
			return game.FollowAddResult{}, fmt.Errorf("record CN local-account follow: %w", err)
		}
	}
	mutual, err := mutualFriendCount(transaction, userID)
	if err != nil {
		return game.FollowAddResult{}, err
	}
	if err := transaction.Commit(); err != nil {
		return game.FollowAddResult{}, fmt.Errorf("commit CN local-account follow: %w", err)
	}
	return game.FollowAddResult{RequestUserIDs: selected, IsFriendFull: mutual >= followMaximum}, nil
}

func mutualFriendCount(transaction *sql.Tx, userID int) (int, error) {
	var count int
	err := transaction.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM cn_local_account_follow AS outbound
  WHERE outbound.follower_user_id = ? AND EXISTS (
   SELECT 1 FROM cn_local_account_follow AS inbound
   WHERE inbound.follower_user_id = outbound.followed_user_id AND inbound.followed_user_id = outbound.follower_user_id
  )`, userID).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count CN local-account mutual friends: %w", err)
	}
	return count, nil
}

func (accounts *Accounts) UnfollowFriendPointAccount(userID int, targetUserID int) error {
	if userID < PrimaryUserID || targetUserID < PrimaryUserID || userID == targetUserID {
		return &game.FollowAddError{ResultCode: -601, ResultString: "无效的取消关注对象，请刷新列表。"}
	}
	accounts.mu.Lock()
	defer accounts.mu.Unlock()
	database, err := accounts.storage.Open()
	if err != nil {
		return err
	}
	result, err := database.ExecContext(
		context.Background(),
		`DELETE FROM cn_local_account_follow
		 WHERE follower_user_id = ? AND followed_user_id = ?`,
		userID,
		targetUserID,
	)
	if err != nil {
		return fmt.Errorf("delete CN local-account follow: %w", err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read CN unfollow affected rows: %w", err)
	}
	if changed > 1 {
		return errors.New("CN unfollow changed multiple relationships")
	}
	// Repeating a successful cancellation already has the requested state.
	return nil
}

func (accounts *Accounts) ListFriendPointRentalEvents(
	userID int,
	afterEventID int64,
) ([]game.FriendPointRentalEvent, error) {
	if userID < PrimaryUserID || afterEventID < 0 {
		return nil, errors.New("invalid CN friend-point inbox cursor")
	}
	database, err := accounts.storage.OpenRead()
	if err != nil {
		return nil, err
	}
	rows, err := database.QueryContext(
		context.Background(),
		`SELECT event_id, renter_user_id, friend_point
		 FROM cn_friend_point_rental
		 WHERE owner_user_id = ? AND event_id > ?
		 ORDER BY event_id`,
		userID,
		afterEventID,
	)
	if err != nil {
		return nil, fmt.Errorf("list CN friend-point rental events: %w", err)
	}
	defer rows.Close()
	events := make([]game.FriendPointRentalEvent, 0)
	for rows.Next() {
		var event game.FriendPointRentalEvent
		if err := rows.Scan(&event.EventID, &event.RenterUserID, &event.FriendPoint); err != nil {
			return nil, fmt.Errorf("scan CN friend-point rental event: %w", err)
		}
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate CN friend-point rental events: %w", err)
	}
	return events, nil
}

func writeFriendPointRentals(
	transaction *sql.Tx,
	eventKey string,
	renterUserID int,
	credits []game.FriendPointRentalCredit,
	bossID int,
) error {
	eventKey = strings.TrimSpace(eventKey)
	if eventKey == "" || len(eventKey) > 256 || renterUserID < PrimaryUserID ||
		bossID <= 0 || len(credits) == 0 || len(credits) > 3 {
		return errors.New("invalid CN friend-point rental event")
	}
	seen := make(map[int]struct{}, len(credits))
	for _, credit := range credits {
		if credit.OwnerUserID < PrimaryUserID || credit.OwnerUserID == renterUserID ||
			credit.FriendPoint <= 0 {
			return errors.New("invalid CN friend-point rental owner")
		}
		if _, duplicate := seen[credit.OwnerUserID]; duplicate {
			return errors.New("duplicate CN friend-point rental owner")
		}
		seen[credit.OwnerUserID] = struct{}{}
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	for _, credit := range credits {
		var exists int
		if err := transaction.QueryRowContext(
			context.Background(),
			`SELECT 1 FROM cn_local_account WHERE user_id = ?`,
			credit.OwnerUserID,
		).Scan(&exists); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return fmt.Errorf("CN friend-point rental owner %d does not exist", credit.OwnerUserID)
			}
			return fmt.Errorf("check CN friend-point rental owner: %w", err)
		}
		if _, err := transaction.ExecContext(
			context.Background(),
			`INSERT INTO cn_friend_point_rental
			 (event_key, owner_user_id, renter_user_id, boss_id, friend_point, created_utc)
			 VALUES (?, ?, ?, ?, ?, ?)
			 ON CONFLICT(event_key, owner_user_id) DO NOTHING`,
			eventKey,
			credit.OwnerUserID,
			renterUserID,
			bossID,
			credit.FriendPoint,
			now,
		); err != nil {
			return fmt.Errorf("record CN friend-point rental event: %w", err)
		}
	}
	return nil
}
