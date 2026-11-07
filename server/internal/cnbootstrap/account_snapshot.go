package cnbootstrap

import (
	"crypto/sha256"
	"encoding/json"

	"kairisei.local/server/internal/accountstore"
	"kairisei.local/server/internal/gamestate"
)

// Master application can mutate slices in place. Capture metadata (including
// inventory component digests) and balances before applying it. This is only
// used when constructing a handler; no extra inventory pass or copy is needed.
func fingerprintCNAccountState(state gamestate.State) ([sha256.Size]byte, error) {
	var result [sha256.Size]byte
	metadata, err := accountstore.EncodeAccountMetadata(state)
	if err != nil {
		return result, err
	}
	digest := sha256.New()
	digest.Write(metadata)
	encoder := json.NewEncoder(digest)
	u := state.User
	if err := encoder.Encode([5]int{u.Gold, u.Coin, u.CoinFree, u.FriendPoint, u.PVPPoint}); err != nil {
		return result, err
	}
	// Metadata includes transactional inventory component digests.
	copy(result[:], digest.Sum(nil))
	return result, nil
}
