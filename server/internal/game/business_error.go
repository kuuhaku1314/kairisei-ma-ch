package game

// Codes from the original CN SV_ERR enum. Keep expected balance failures out
// of HTTP transport errors, which the original client treats as disconnection.
type BusinessError struct {
	Code    int
	Message string
}

func (e *BusinessError) Error() string { return e.Message }

var (
	ErrInsufficientCrystals     = &BusinessError{-1060, "水晶不足，请补充后再试。"}
	errInsufficientPaidCrystals = &BusinessError{-1060, "付费水晶不足，请补充后再试。"}
	ErrInsufficientGold         = &BusinessError{-1030, "金币不足。"}
	ErrInsufficientMaterials    = &BusinessError{-1200, "素材或道具不足，请重新选择。"}
	ErrCardUnavailable          = &BusinessError{-1, "所选卡牌已不存在，请刷新后重新选择。"}
	ErrCardInDeck               = &BusinessError{-1, "所选卡牌正在卡组中使用，请先从卡组中移除。"}
	ErrCardLocked               = &BusinessError{-1, "所选卡牌已锁定，请先解锁。"}
	errSphereUnavailable        = &BusinessError{-1, "所选秘石已不存在，请刷新后重新选择。"}
	errSphereLocked             = &BusinessError{-1, "所选秘石已锁定，请先解锁。"}
	errSphereInDeck             = &BusinessError{-1, "所选秘石正在卡组中使用，请先从卡组中移除。"}
	ErrBuddyUnavailable         = &BusinessError{-1, "所选传承卡已不存在，请刷新后重新选择。"}
	ErrBuddyLocked              = &BusinessError{-1, "所选传承卡已锁定，请先解锁。"}
	errBuddyInDeck              = &BusinessError{-1, "所选传承卡正在卡组中使用，请先从卡组中移除。"}
	errInsufficientFriendPoints = &BusinessError{-1, "友情点不足。"}
	errInsufficientStive        = &BusinessError{-1, "名声训练点数不足。"}
	ErrItemExpired              = &BusinessError{-1201, "道具已过期。"}
	ErrCardCapacity             = &BusinessError{-2900, "卡牌容量不足，请整理卡牌后再试。"}
	ErrSphereCapacity           = &BusinessError{-2902, "圣剑容量不足，请整理后再试。"}
	ErrBuddyCapacity            = &BusinessError{-2903, "传承卡容量不足，请整理后再试。"}
	errItemCapacity             = &BusinessError{-1, "道具持有数量已达上限。"}
	ErrGachaUnavailable         = &BusinessError{-3100, "这个扭蛋暂不可用，请重新选择。"}
	errGachaDailyLimit          = &BusinessError{-1, "已达到该卡池今日的抽取上限。"}
	errCrystalCapacity          = &BusinessError{-1061, "水晶持有数量已达上限。"}
)
