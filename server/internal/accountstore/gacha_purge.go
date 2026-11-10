package accountstore

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"

	"kairisei.local/server/internal/gamestate"
)

// Operations builds this only for a deleted operator pool. Storage limits the
// transaction to that pool's documents, counters and the two registry owners.
type GachaPurgePlan struct {
	GroupID  int             `json:"group_id"`
	GachaIDs []int           `json:"gacha_ids"`
	Writes   []DocumentWrite `json:"writes"`
}

type purgeDocument struct {
	Key string `json:"document_key"`
	Document
	Remove bool `json:"remove"`
}

func (d *Database) PurgeGachaPool(ctx context.Context, plan GachaPurgePlan, expected string) (OperationalCleanupReport, error) {
	return d.cleanupOperationalState(ctx, OperationalReferences{}, expected, &plan)
}

func gachaPurgeDocuments(ctx context.Context, tx *sql.Tx, plan *GachaPurgePlan) ([]purgeDocument, error) {
	if plan.GroupID < gamestate.OperatorGachaFirstID || len(plan.GachaIDs) == 0 || len(plan.Writes) == 0 {
		return nil, errors.New("invalid gacha purge plan")
	}
	seen := map[string]bool{}
	docs := []purgeDocument{}
	hasRegistry := false
	read := func(key string, remove bool, expected int) error {
		if seen[key] {
			return errors.New("duplicate purge document")
		}
		seen[key] = true
		row := purgeDocument{Key: key, Remove: remove}
		err := tx.QueryRowContext(ctx, `SELECT revision,updated_utc,payload_json,payload_sha256 FROM cn_global_operation WHERE operation_key=?`, key).Scan(&row.Revision, &row.UpdatedUTC, &row.Payload, &row.SHA256)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if expected >= 0 && row.Revision != expected {
			return ErrDocumentConflict
		}
		if row.Revision > 0 {
			sum := sha256.Sum256(row.Payload)
			if hex.EncodeToString(sum[:]) != row.SHA256 {
				return errors.New("purge configuration digest mismatch")
			}
		}
		docs = append(docs, row)
		return nil
	}
	for _, write := range plan.Writes {
		if write.Expected < 0 {
			return nil, ErrDocumentConflict
		}
		if write.Key != "gacha-custom" && write.Key != "gacha_publication" {
			return nil, errors.New("purge cannot change unrelated configuration")
		}
		hasRegistry = hasRegistry || write.Key == "gacha-custom"
		if err := read(write.Key, false, write.Expected); err != nil {
			return nil, err
		}
	}
	if !hasRegistry {
		return nil, errors.New("purge registry update is required")
	}
	for _, id := range plan.GachaIDs {
		if id < gamestate.OperatorGachaFirstID || gamestate.IsOnboardingGachaID(id) {
			return nil, errors.New("purge cannot remove a built-in pool")
		}
		for _, prefix := range []string{"gacha-draft:", "gacha-live:"} {
			if err := read(prefix+strconv.Itoa(id), true, -1); err != nil {
				return nil, err
			}
		}
	}
	return docs, nil
}

func applyGachaPurge(ctx context.Context, tx *sql.Tx, plan *GachaPurgePlan, docs []purgeDocument, stamp string) error {
	for _, write := range plan.Writes {
		body, err := json.Marshal(write.Value)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(body)
		doc := Document{Revision: write.Expected + 1, UpdatedUTC: stamp, Payload: body, SHA256: hex.EncodeToString(sum[:])}
		if err := writeDocumentTx(tx, write, doc); err != nil {
			return err
		}
	}
	for _, doc := range docs {
		if doc.Remove && doc.Revision > 0 {
			result, err := tx.ExecContext(ctx, `DELETE FROM cn_global_operation WHERE operation_key=? AND revision=?`, doc.Key, doc.Revision)
			if err != nil {
				return err
			}
			n, err := result.RowsAffected()
			if err != nil {
				return err
			}
			if n != 1 {
				return ErrDocumentConflict
			}
		}
	}
	return nil
}
