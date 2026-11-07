package battlecore

// ContractResultCodec supplies the original consumer boundary to the offline
// contract suite. Engine construction and gameplay never use this dependency.
// Keeping it explicit lets the same contracts check a codec without importing
// a transport adapter into the core or maintaining a second encoder.
type ContractResultCodec struct {
	Row   func(BattleResult) (string, error)
	Group func([]BattleResult) (string, error)
}
