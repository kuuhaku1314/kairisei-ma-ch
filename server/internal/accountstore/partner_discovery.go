package accountstore

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"kairisei.local/server/internal/game"
)

const recentPartnerAccountLimit = 100

// A top-100 maximum of login/update times must be in the union of the top
// 100 of either time. Each branch uses its own index; only the bounded union
// is sorted together. Preferential accounts never consume the recent quota.
const partnerRecommendationSelection = `WITH
recent_login AS (
 SELECT a.user_id
 FROM cn_local_account a INDEXED BY cn_local_account_recent_login
 WHERE a.user_id <> ?1 AND a.user_id < ?2
   AND EXISTS (SELECT 1 FROM cn_account_projection p WHERE p.user_id = a.user_id)
   AND NOT EXISTS (SELECT 1 FROM cn_local_account_follow f
                   WHERE f.follower_user_id = ?1 AND f.followed_user_id = a.user_id)
 ORDER BY julianday(a.last_login_utc) DESC, a.user_id DESC LIMIT ?3
),
recent_update AS (
 SELECT p.user_id
 FROM cn_account_projection p INDEXED BY cn_account_projection_recent_update
 WHERE p.user_id <> ?1 AND p.user_id < ?2
   AND EXISTS (SELECT 1 FROM cn_local_account a WHERE a.user_id = p.user_id)
   AND NOT EXISTS (SELECT 1 FROM cn_local_account_follow f
                   WHERE f.follower_user_id = ?1 AND f.followed_user_id = p.user_id)
 ORDER BY julianday(p.updated_utc) DESC, p.user_id DESC LIMIT ?3
),
recent_ids AS (
 SELECT user_id FROM recent_login UNION SELECT user_id FROM recent_update
),
recent AS (
 SELECT r.user_id
 FROM recent_ids r
 JOIN cn_local_account a ON a.user_id = r.user_id
 JOIN cn_account_projection p ON p.user_id = r.user_id
 ORDER BY max(julianday(a.last_login_utc), julianday(p.updated_utc)) DESC,
          r.user_id DESC LIMIT ?3
),
selected(user_id) AS (
 SELECT followed_user_id FROM cn_local_account_follow WHERE follower_user_id = ?1
 UNION SELECT user_id FROM recent
 UNION SELECT user_id FROM cn_local_account WHERE user_id > ?2 AND user_id <= ?4
)
`

const partnerRelationProjection = `SELECT a.user_id, p.payload_json, p.payload_sha256, a.last_login_utc,
 EXISTS (SELECT 1 FROM cn_local_account_follow f
         WHERE f.follower_user_id = ?1 AND f.followed_user_id = a.user_id),
 EXISTS (SELECT 1 FROM cn_local_account_follow f
         WHERE f.follower_user_id = a.user_id AND f.followed_user_id = ?1)
 FROM selected s
 JOIN cn_local_account a ON a.user_id = s.user_id
 JOIN cn_account_projection p ON p.user_id = s.user_id
 WHERE a.user_id <> ?1`

func (accounts *Accounts) ListFriendPointPartnerRecommendations(userID int) ([]game.FriendPointAccountRelation, error) {
	return accounts.queryPartnerRelations(userID, partnerRecommendationSelection+partnerRelationProjection+" ORDER BY a.user_id",
		userID, SystemPartnerUserIDBase, recentPartnerAccountLimit, SystemPartnerUserIDBase+systemPartnerArthurCount)
}

// A previously displayed selection stays usable when other players enter the
// recent window. Rental details/start reload only the requested accounts and
// still validate their current deck; discovery is not an authorization token.
func (accounts *Accounts) LoadFriendPointAccountRelations(userID int, targetUserIDs []int) ([]game.FriendPointAccountRelation, error) {
	seen := make(map[int]bool, len(targetUserIDs))
	values := make([]string, 0, len(targetUserIDs))
	arguments := []any{userID}
	for _, target := range targetUserIDs {
		if target < PrimaryUserID || target == userID || seen[target] {
			continue
		}
		seen[target] = true
		arguments = append(arguments, target)
		values = append(values, fmt.Sprintf("(?%d)", len(arguments)))
	}
	if len(values) == 0 {
		return nil, nil
	}
	selection := "WITH selected(user_id) AS (VALUES " + strings.Join(values, ",") + ")\n"
	return accounts.queryPartnerRelations(userID, selection+partnerRelationProjection+" ORDER BY a.user_id", arguments...)
}

func (accounts *Accounts) queryPartnerRelations(userID int, query string, arguments ...any) ([]game.FriendPointAccountRelation, error) {
	if userID < PrimaryUserID {
		return nil, errors.New("invalid CN partner relationship owner")
	}
	database, err := accounts.storage.OpenRead()
	if err != nil {
		return nil, err
	}
	rows, err := database.QueryContext(context.Background(), query, arguments...)
	if err != nil {
		return nil, fmt.Errorf("query CN partner accounts: %w", err)
	}
	defer rows.Close()
	result := make([]game.FriendPointAccountRelation, 0)
	for rows.Next() {
		var target int
		var content []byte
		var digest, lastLoginUTC string
		var follows, followedBy bool
		if err := rows.Scan(&target, &content, &digest, &lastLoginUTC, &follows, &followedBy); err != nil {
			return nil, fmt.Errorf("scan CN partner account: %w", err)
		}
		state, err := DecodeAccountProjection(content, digest)
		if err != nil || state.User.UserID != target {
			return nil, fmt.Errorf("invalid CN partner projection %d: %v", target, err)
		}
		lastLogin, err := time.Parse(time.RFC3339Nano, lastLoginUTC)
		if err != nil {
			return nil, fmt.Errorf("decode CN partner %d login time: %w", target, err)
		}
		state.LastLoginUnix = lastLogin.Unix()
		system := isSystemPartnerUserID(target)
		if system {
			state.LastLoginUnix = time.Now().Unix()
		}
		friendState := game.FriendStateOther
		switch {
		case follows && followedBy:
			friendState = game.FriendStateFriend
		case follows:
			friendState = game.FriendStateFollow
		case followedBy:
			friendState = game.FriendStateFollower
		}
		result = append(result, game.FriendPointAccountRelation{State: state, FriendState: friendState, System: system})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate CN partner accounts: %w", err)
	}
	return result, nil
}
