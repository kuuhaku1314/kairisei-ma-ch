package gamestate

// Account battle admission roles, independent of the accepted entry price.
// Zero is solo; multiplayer admission records the explicit host/guest role.
type BattleEntryRole int8

const (
	BattleEntrySolo BattleEntryRole = iota
	BattleEntryMultiplayerHost
	BattleEntryMultiplayerGuest
)

func (role BattleEntryRole) PaysEntryCost() bool {
	return role != BattleEntryMultiplayerGuest
}

func (role BattleEntryRole) IsMultiplayer() bool {
	return role == BattleEntryMultiplayerHost || role == BattleEntryMultiplayerGuest
}
