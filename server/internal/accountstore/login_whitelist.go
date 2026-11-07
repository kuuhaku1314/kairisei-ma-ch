package accountstore

import (
	"errors"
	"slices"
)

const MaxLoginWhitelistUsers = 2000

var ErrLoginWhitelist = errors.New("服务器已开启白名单登录，该账号未获准登录")

// LoginWhitelist is immutable after construction. An ID of zero represents
// a login or binding that would have to create a new player.
type LoginWhitelist struct {
	enabled bool
	userIDs []int
}

func CompileLoginWhitelist(enabled bool, userIDs []int) (LoginWhitelist, error) {
	if len(userIDs) > MaxLoginWhitelistUsers {
		return LoginWhitelist{}, errors.New("登录白名单最多 2000 名玩家")
	}
	ids := slices.Clone(userIDs)
	for _, id := range ids {
		if id < PrimaryUserID || id >= SystemPartnerUserIDBase {
			return LoginWhitelist{}, errors.New("登录白名单须使用有效的玩家 ID")
		}
	}
	slices.Sort(ids)
	return LoginWhitelist{enabled: enabled, userIDs: slices.Compact(ids)}, nil
}

func (p LoginWhitelist) Enabled() bool { return p.enabled }

func (p LoginWhitelist) UserIDs() []int {
	return append([]int{}, p.userIDs...)
}

func (p LoginWhitelist) Allows(userID int) bool {
	if !p.enabled {
		return true
	}
	_, allowed := slices.BinarySearch(p.userIDs, userID)
	return allowed
}
