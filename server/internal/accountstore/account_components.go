package accountstore

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"

	"kairisei.local/server/internal/gamestate"
)

// Optional storage hints, committed with their rows. Old metadata has no
// hints and takes the existing complete comparison path on its next save.
// No in-memory cache can get ahead of a rolled-back transaction.
type accountComponentHashes struct {
	Cards    string `json:"cards_sha256"`
	Items    string `json:"items_sha256"`
	Stacks   string `json:"stacks_sha256"`
	Presents string `json:"presents_sha256"`
}

func accountComponentDigests(state gamestate.State) (accountComponentHashes, error) {
	var result accountComponentHashes
	components := []struct {
		value  any
		digest *string
	}{
		{[2][]gamestate.Card{state.Cards, state.ContainerCards}, &result.Cards},
		{state.Items, &result.Items},
		{state.StackCards, &result.Stacks},
		{[2][]gamestate.Present{state.Engagement.Presents, state.Engagement.Histories}, &result.Presents},
	}
	for _, component := range components {
		hash := sha256.New()
		if err := json.NewEncoder(hash).Encode(component.value); err != nil {
			return result, err
		}
		*component.digest = hex.EncodeToString(hash.Sum(nil))
	}
	return result, nil
}

func changedAccountComponents(tx *sql.Tx, userID int, content []byte) (accountComponentHashes, accountComponentHashes, error) {
	var before, after struct {
		Components accountComponentHashes `json:"row_components"`
	}
	if err := json.Unmarshal(content, &after); err != nil {
		return before.Components, after.Components, err
	}
	var old []byte
	var err error
	if userID == PrimaryUserID {
		err = tx.QueryRow(`SELECT payload_json FROM cn_save_snapshot WHERE singleton=1`).Scan(&old)
	} else {
		err = tx.QueryRow(`SELECT payload_json FROM cn_account_snapshot WHERE user_id=?`, userID).Scan(&old)
	}
	if errors.Is(err, sql.ErrNoRows) {
		return before.Components, after.Components, nil
	}
	if err == nil {
		err = json.Unmarshal(old, &before)
	}
	return before.Components, after.Components, err
}
