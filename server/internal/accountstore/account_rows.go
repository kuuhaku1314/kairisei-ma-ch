package accountstore

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"kairisei.local/server/internal/gamestate"
)

type storedRow struct {
	id     int64
	values []any
}

// Table and column names below are private constants, never request input.
// Read only identities/digests, then insert/update/delete changed rows. This
// avoids thousands of no-op SQL updates on every draw, rename or page change.
func syncAccountRows(tx *sql.Tx, table, key, columns string, userID int, incoming []storedRow) error {
	rows, err := tx.Query("SELECT "+key+", row_sha256, sort_order FROM "+table+" WHERE user_id = ?", userID)
	if err != nil {
		return err
	}
	previous := make(map[int64]string)
	previousOrder := make(map[int64]int64)
	for rows.Next() {
		var id int64
		var hash string
		var order int64
		if err := rows.Scan(&id, &hash, &order); err != nil {
			_ = rows.Close()
			return err
		}
		previous[id] = hash
		previousOrder[id] = order
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	fields := strings.Split(columns+",sort_order", ",")
	assignments := make([]string, 0, len(fields)+1)
	for _, field := range fields {
		assignments = append(assignments, field+"=excluded."+field)
	}
	assignments = append(assignments, "row_sha256=excluded.row_sha256")
	statement := "INSERT INTO " + table + " (user_id," + key + "," + columns + ",sort_order,row_sha256) VALUES (?" +
		strings.Repeat(",?", len(fields)+2) + ") ON CONFLICT(user_id," + key + ") DO UPDATE SET " + strings.Join(assignments, ",")
	var update, remove *sql.Stmt
	defer func() {
		if update != nil {
			_ = update.Close()
		}
		if remove != nil {
			_ = remove.Close()
		}
	}()
	seen := make(map[int64]bool, len(incoming))
	order := stableRowOrder(incoming, previousOrder)
	for i, row := range incoming {
		if row.id <= 0 || seen[row.id] || len(row.values) != len(fields)-1 {
			return fmt.Errorf("invalid %s row identity or columns", table)
		}
		seen[row.id] = true
		row.values = append(row.values, order[i])
		raw, err := json.Marshal(row.values)
		if err != nil {
			return err
		}
		digest := sha256.Sum256(raw)
		hash := hex.EncodeToString(digest[:])
		old, exists := previous[row.id]
		delete(previous, row.id)
		if exists && old == hash {
			continue
		}
		if update == nil {
			update, err = tx.Prepare(statement)
			if err != nil {
				return err
			}
		}
		values := make([]any, 0, len(row.values)+3)
		values = append(values, userID, row.id)
		values = append(values, row.values...)
		values = append(values, hash)
		if _, err := update.Exec(values...); err != nil {
			return fmt.Errorf("write %s: %w", table, err)
		}
	}
	for id := range previous {
		if remove == nil {
			remove, err = tx.Prepare("DELETE FROM " + table + " WHERE user_id = ? AND " + key + " = ?")
			if err != nil {
				return err
			}
		}
		if _, err := remove.Exec(userID, id); err != nil {
			return err
		}
	}
	return nil
}

// Preserve list order without renumbering all rows after a sale or claim.
// Gaps permit insertions; an actual reorder is the only common reindex case.
func stableRowOrder(rows []storedRow, previous map[int64]int64) []int64 {
	order, next := make([]int64, len(rows)), make([]int64, len(rows))
	var following int64
	for i := len(rows) - 1; i >= 0; i-- {
		next[i] = following
		if old := previous[rows[i].id]; old > 0 {
			following = old
		}
	}
	var last int64
	for i, row := range rows {
		value := previous[row.id]
		if value == 0 {
			value = last + 1024
			if next[i] > 0 {
				value = last + (next[i]-last)/2
			}
		}
		if value <= last || value > 1<<60 {
			for j := range rows {
				order[j] = int64(j+1) * 1024
			}
			return order
		}
		order[i], last = value, value
	}
	return order
}

const cardRowColumns = "slot,card_id,level,experience,now_exp,next_exp,love,fame,is_lock,hp,attack,magic,mind,base_add_price,skill_levels,create_time"

const itemRowColumns = "num,limit_time"

const stackRowColumns = "num,hp,attack,magic,mind,add_exp,base_add_price,material_type"

const presentRowColumns = "history,issued_at_unix,add_elapsed_sec,limit_time,state,reason,title,comment,url,auto_fusion_used,auto_loveup_used,admin_key,rewards_json"

func writeAccountRows(tx *sql.Tx, state gamestate.State) error {
	return writeChangedAccountRows(tx, state, accountComponentHashes{}, accountComponentHashes{})
}

func writeSnapshotRows(tx *sql.Tx, state gamestate.State, content []byte) error {
	before, after, err := changedAccountComponents(tx, state.User.UserID, content)
	if err != nil {
		return err
	}
	return writeChangedAccountRows(tx, state, before, after)
}

func writeChangedAccountRows(tx *sql.Tx, state gamestate.State, before, after accountComponentHashes) error {
	u := state.User
	if _, err := tx.Exec(`INSERT INTO cn_account_balance(user_id,gold,coin,coin_free,friend_point,pvp_point)
 VALUES(?,?,?,?,?,?) ON CONFLICT(user_id) DO UPDATE SET gold=excluded.gold,coin=excluded.coin,
 coin_free=excluded.coin_free,friend_point=excluded.friend_point,pvp_point=excluded.pvp_point
 WHERE gold<>excluded.gold OR coin<>excluded.coin OR coin_free<>excluded.coin_free
 OR friend_point<>excluded.friend_point OR pvp_point<>excluded.pvp_point`,
		u.UserID, u.Gold, u.Coin, u.CoinFree, u.FriendPoint, u.PVPPoint); err != nil {
		return err
	}
	if after.Cards == "" || after.Cards != before.Cards {
		cards := make([]storedRow, 0, len(state.Cards)+len(state.ContainerCards))
		for slot, inventory := range [][]gamestate.Card{state.Cards, state.ContainerCards} {
			for _, c := range inventory {
				skills, err := json.Marshal(c.SkillLevels)
				if err != nil {
					return err
				}
				cards = append(cards, storedRow{c.UniqueID, []any{slot, c.CardID, c.Level, c.Experience, c.NowLevelExperience, c.NextLevelExperience, c.Love, c.Fame, c.IsLock, c.HP, c.Attack, c.Magic, c.Mind, c.BaseAddPrice, string(skills), c.CreateTime}})
			}
		}
		if err := syncAccountRows(tx, "cn_account_card", "unique_id", cardRowColumns, u.UserID, cards); err != nil {
			return err
		}
	}
	if after.Items == "" || after.Items != before.Items {
		items := make([]storedRow, 0, len(state.Items))
		for _, item := range state.Items {
			items = append(items, storedRow{int64(item.ItemID), []any{item.Num, item.LimitTime}})
		}
		if err := syncAccountRows(tx, "cn_account_item", "item_id", itemRowColumns, u.UserID, items); err != nil {
			return err
		}
	}
	if after.Stacks == "" || after.Stacks != before.Stacks {
		stacks := make([]storedRow, 0, len(state.StackCards))
		for _, c := range state.StackCards {
			stacks = append(stacks, storedRow{int64(c.CardID), []any{c.Num, c.HP, c.Attack, c.Magic, c.Mind, c.AddExperience, c.BaseAddPrice, c.MaterialType}})
		}
		if err := syncAccountRows(tx, "cn_account_stack_card", "card_id", stackRowColumns, u.UserID, stacks); err != nil {
			return err
		}
	}
	if after.Presents == "" || after.Presents != before.Presents {
		presents := make([]storedRow, 0, len(state.Engagement.Presents)+len(state.Engagement.Histories))
		for history, group := range [][]gamestate.Present{state.Engagement.Presents, state.Engagement.Histories} {
			for _, p := range group {
				// Only the four small, typed reward descriptors are variable JSON.
				rewards, err := json.Marshal([4]gamestate.Reward{p.Reward, p.Reward0, p.Reward1, p.Reward2})
				if err != nil {
					return err
				}
				presents = append(presents, storedRow{p.PresentID, []any{history, p.IssuedAtUnix, p.AddElapsedSec, p.LimitTime, p.State, p.Reason, p.Title, p.Comment, p.URL, p.AutoFusionUsed, p.AutoLoveupUsed, p.AdminIdempotencyKey, string(rewards)}})
			}
		}
		if err := syncAccountRows(tx, "cn_account_present", "present_id", presentRowColumns, u.UserID, presents); err != nil {
			return err
		}
	}
	return nil
}

// Metadata and all row tables must be read in one transaction; otherwise a
// concurrent save could combine an old balance with new inventory.
func readAccountRows(tx *sql.Tx, state *gamestate.State) error {
	u := &state.User
	if err := tx.QueryRow(`SELECT gold,coin,coin_free,friend_point,pvp_point FROM cn_account_balance WHERE user_id=?`, u.UserID).
		Scan(&u.Gold, &u.Coin, &u.CoinFree, &u.FriendPoint, &u.PVPPoint); err != nil {
		return fmt.Errorf("read account balances: %w", err)
	}
	templates := make(map[int]gamestate.Card, len(state.CardTemplates)+len(state.Cards))
	for _, inventory := range [][]gamestate.Card{state.Cards, state.ContainerCards, state.CardTemplates} {
		for _, card := range inventory {
			templates[card.CardID] = card
		}
	}
	state.Cards, state.ContainerCards = []gamestate.Card{}, []gamestate.Card{}
	read := func(table, key, columns string, scan func(*sql.Rows) error) error {
		rows, err := tx.Query("SELECT "+key+","+columns+" FROM "+table+" WHERE user_id=? ORDER BY sort_order,"+key, u.UserID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			if err := scan(rows); err != nil {
				return err
			}
		}
		return rows.Err()
	}
	if err := read("cn_account_card", "unique_id", cardRowColumns, func(rows *sql.Rows) error {
		var c gamestate.Card
		var slot int
		var skills string
		if err := rows.Scan(&c.UniqueID, &slot, &c.CardID, &c.Level, &c.Experience, &c.NowLevelExperience, &c.NextLevelExperience, &c.Love, &c.Fame, &c.IsLock, &c.HP, &c.Attack, &c.Magic, &c.Mind, &c.BaseAddPrice, &skills, &c.CreateTime); err != nil {
			return err
		}
		if len(skills) > 4096 {
			return errors.New("oversized card skill levels")
		}
		if err := json.Unmarshal([]byte(skills), &c.SkillLevels); err != nil {
			return err
		}
		template, found := templates[c.CardID]
		if !found {
			return fmt.Errorf("account card %d has no public template", c.CardID)
		}
		template.UniqueID, template.Level, template.Experience = c.UniqueID, c.Level, c.Experience
		template.CreateTime = c.CreateTime
		template.NowLevelExperience, template.NextLevelExperience = c.NowLevelExperience, c.NextLevelExperience
		template.Love, template.Fame, template.IsLock = c.Love, c.Fame, c.IsLock
		template.HP, template.Attack, template.Magic, template.Mind = c.HP, c.Attack, c.Magic, c.Mind
		template.BaseAddPrice, template.SkillLevels = c.BaseAddPrice, c.SkillLevels
		if slot == 0 {
			state.Cards = append(state.Cards, template)
		} else {
			state.ContainerCards = append(state.ContainerCards, template)
		}
		return nil
	}); err != nil {
		return err
	}
	state.Items = []gamestate.Item{}
	if err := read("cn_account_item", "item_id", itemRowColumns, func(rows *sql.Rows) error {
		var item gamestate.Item
		if err := rows.Scan(&item.ItemID, &item.Num, &item.LimitTime); err != nil {
			return err
		}
		state.Items = append(state.Items, item)
		return nil
	}); err != nil {
		return err
	}
	state.StackCards = []gamestate.CardStack{}
	if err := read("cn_account_stack_card", "card_id", stackRowColumns, func(rows *sql.Rows) error {
		var c gamestate.CardStack
		if err := rows.Scan(&c.CardID, &c.Num, &c.HP, &c.Attack, &c.Magic, &c.Mind, &c.AddExperience, &c.BaseAddPrice, &c.MaterialType); err != nil {
			return err
		}
		state.StackCards = append(state.StackCards, c)
		return nil
	}); err != nil {
		return err
	}
	state.Engagement.Presents, state.Engagement.Histories = []gamestate.Present{}, []gamestate.Present{}
	return read("cn_account_present", "present_id", presentRowColumns, func(rows *sql.Rows) error {
		var p gamestate.Present
		var history int
		var raw string
		if err := rows.Scan(&p.PresentID, &history, &p.IssuedAtUnix, &p.AddElapsedSec, &p.LimitTime, &p.State, &p.Reason, &p.Title, &p.Comment, &p.URL, &p.AutoFusionUsed, &p.AutoLoveupUsed, &p.AdminIdempotencyKey, &raw); err != nil {
			return err
		}
		if len(raw) > 64*1024 {
			return errors.New("oversized present reward descriptors")
		}
		var rewards [4]gamestate.Reward
		if err := json.Unmarshal([]byte(raw), &rewards); err != nil {
			return err
		}
		p.Reward, p.Reward0, p.Reward1, p.Reward2 = rewards[0], rewards[1], rewards[2], rewards[3]
		if history == 0 {
			state.Engagement.Presents = append(state.Engagement.Presents, p)
		} else {
			state.Engagement.Histories = append(state.Engagement.Histories, p)
		}
		return nil
	})
}
