package accountstore

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"kairisei.local/server/internal/gamestate"
)

const (
	PrimaryUserID            = 1000001
	SystemPartnerUserIDBase  = 1900000000
	systemPartnerArthurCount = 4
)

var SystemPartnerNameByArthur = map[int8]string{
	1: "新手伙伴·佣兵",
	2: "新手伙伴·富豪",
	3: "新手伙伴·盗贼",
	4: "新手伙伴·歌姬",
}

type accountIdentity struct {
	UserID     int
	LoginUUID  string
	SessionKey string
}

type Accounts struct {
	creation     atomic.Pointer[creationGuard]
	giftMu       sync.Mutex
	giftAttempts map[int]time.Time
	storage      *Database
	mu           sync.Mutex
	battleMu     sync.Mutex
	battles      map[int]*accountBattleSession
}

var ErrInvalidSession = errors.New("CN account session is invalid")

// InvalidateSessions starts a new serving lifetime. Persisted account/login
// identities and saves survive, but a client must return through login and
// resource checking after a restart. Short, unique markers cannot authenticate.
// Call once before accepting HTTP or BattleSv traffic, never per request.
func (accounts *Accounts) InvalidateSessions() error {
	database, err := accounts.storage.Open()
	if err != nil {
		return err
	}
	_, err = database.Exec(`UPDATE cn_local_account SET session_key = 'expired:' || user_id`)
	if err != nil {
		return fmt.Errorf("invalidate previous serving lifetime sessions: %w", err)
	}
	return nil
}

// A solo battle needs only a small runtime context, not a retained account
// handler or a durable participation lock. Abandoned sessions expire after a
// hour without a successful account mutation; process exit drops them all.
const accountBattleLifetime = time.Hour

type accountBattleSession struct {
	state *gamestate.TeamBattleActiveState
	story gamestate.StoryTeamBattleSession
	timer *time.Timer
}

func (accounts *Accounts) RememberBattle(userID int, active *gamestate.TeamBattleActiveState, story gamestate.StoryTeamBattleSession) {
	accounts.battleMu.Lock()
	defer accounts.battleMu.Unlock()
	if previous := accounts.battles[userID]; previous != nil {
		previous.timer.Stop()
		delete(accounts.battles, userID)
	}
	if active == nil && story.StoryID == 0 {
		return
	}
	if accounts.battles == nil {
		accounts.battles = make(map[int]*accountBattleSession)
	}
	session := &accountBattleSession{state: cloneActiveTeamBattleState(active), story: story}
	accounts.battles[userID] = session
	session.timer = time.AfterFunc(accountBattleLifetime, func() {
		accounts.battleMu.Lock()
		defer accounts.battleMu.Unlock()
		if accounts.battles[userID] == session {
			delete(accounts.battles, userID)
		}
	})
}

// Caller has drained account writers and will evict their cached handlers.
// These are serving-lifetime runs, never settled receipts or login sessions.
func (accounts *Accounts) DiscardUnsettledBattles() int {
	accounts.battleMu.Lock()
	defer accounts.battleMu.Unlock()
	count := len(accounts.battles)
	for _, session := range accounts.battles {
		session.timer.Stop()
	}
	clear(accounts.battles)
	return count
}

func NewAccounts(storage *Database) (*Accounts, error) {
	if storage == nil {
		return nil, errors.New("CN save database is required")
	}
	if err := storage.EnsureSchema(); err != nil {
		return nil, err
	}
	accounts := &Accounts{storage: storage}
	database, err := storage.Open()
	if err != nil {
		return nil, err
	}
	transaction, err := database.BeginTx(context.Background(), &sql.TxOptions{})
	if err != nil {
		return nil, fmt.Errorf("begin CN account schema initialization: %w", err)
	}
	if err := accounts.ensureSystemPartnerAccounts(transaction); err != nil {
		_ = transaction.Rollback()
		return nil, err
	}
	if err := storage.syncAccountProjections(transaction); err != nil {
		_ = transaction.Rollback()
		return nil, err
	}
	if err := transaction.Commit(); err != nil {
		return nil, fmt.Errorf("commit CN account schema initialization: %w", err)
	}
	return accounts, nil
}

func SystemPartnerUserID(arthurType int8) int {
	return SystemPartnerUserIDBase + int(arthurType)
}

func isSystemPartnerUserID(userID int) bool {
	return userID > SystemPartnerUserIDBase &&
		userID <= SystemPartnerUserIDBase+systemPartnerArthurCount
}

func InviteID(userID int) string {
	if isSystemPartnerUserID(userID) {
		return fmt.Sprintf("%09d", 900000000+userID-SystemPartnerUserIDBase)
	}
	return fmt.Sprintf("%09d", userID)
}

// cnSystemPartnerLoadoutFromSeed keeps the versioned, profession-specific
// battle loadout separate from the deliberately small new-player inventory.
// These decks remain an INFERRED local fallback rather than a recovered
// retired-service partner composition.
func SystemPartnerLoadoutFromSeed(
	seed gamestate.State,
	arthurType int8,
) ([]gamestate.Card, gamestate.Deck, gamestate.Card, error) {
	if arthurType < 1 || arthurType > systemPartnerArthurCount {
		return nil, gamestate.Deck{}, gamestate.Card{}, errors.New("CN system-partner Arthur type is invalid")
	}

	var sourceDeck *gamestate.Deck
	for index := range seed.Decks {
		deck := &seed.Decks[index]
		if deck.ArthurType != arthurType || deck.JobType != arthurType || deck.Index != 0 {
			continue
		}
		if sourceDeck == nil || deck.IsActive != 0 {
			sourceDeck = deck
		}
	}
	if sourceDeck == nil || len(sourceDeck.CardUniqueIDs) != 10 {
		return nil, gamestate.Deck{}, gamestate.Card{}, fmt.Errorf(
			"CN system-partner seed has no complete Arthur type %d deck",
			arthurType,
		)
	}
	leaderIndex := int(sourceDeck.LeaderCardIndex)
	if leaderIndex < 0 || leaderIndex >= len(sourceDeck.CardUniqueIDs) {
		return nil, gamestate.Deck{}, gamestate.Card{}, fmt.Errorf(
			"CN system-partner Arthur type %d leader index is invalid",
			arthurType,
		)
	}

	seedCards := make(map[int64]gamestate.Card, len(seed.Cards))
	for _, card := range seed.Cards {
		if card.UniqueID <= 0 {
			continue
		}
		if _, duplicate := seedCards[card.UniqueID]; duplicate {
			return nil, gamestate.Deck{}, gamestate.Card{}, fmt.Errorf(
				"CN system-partner seed card unique ID %d is duplicated",
				card.UniqueID,
			)
		}
		seedCards[card.UniqueID] = card
	}

	cards := make([]gamestate.Card, 0, len(sourceDeck.CardUniqueIDs))
	seen := make(map[int64]struct{}, len(sourceDeck.CardUniqueIDs))
	for _, uniqueID := range sourceDeck.CardUniqueIDs {
		if uniqueID <= 0 {
			return nil, gamestate.Deck{}, gamestate.Card{}, fmt.Errorf(
				"CN system-partner Arthur type %d deck contains an empty card slot",
				arthurType,
			)
		}
		if _, duplicate := seen[uniqueID]; duplicate {
			return nil, gamestate.Deck{}, gamestate.Card{}, fmt.Errorf(
				"CN system-partner Arthur type %d deck repeats card unique ID %d",
				arthurType,
				uniqueID,
			)
		}
		card, exists := seedCards[uniqueID]
		if !exists {
			return nil, gamestate.Deck{}, gamestate.Card{}, fmt.Errorf(
				"CN system-partner Arthur type %d deck card %d is unavailable",
				arthurType,
				uniqueID,
			)
		}
		card.SkillLevels = append([]int16(nil), card.SkillLevels...)
		cards = append(cards, card)
		seen[uniqueID] = struct{}{}
	}

	deck := cloneReleaseDeck(*sourceDeck)
	deck.SupportCardUniqueIDs = make([]int64, len(deck.SupportCardUniqueIDs))
	deck.SphereUniqueIDs = []int64{}
	deck.BuddyUniqueIDs = make([]int64, 5)
	deck.Name = DefaultDeckNameByArthur[arthurType]
	deck.IsActive = 1
	deck.IsRental = 0
	return cards, deck, cards[leaderIndex], nil
}

func SystemPartnerPopulationFromSeed(
	seed gamestate.State,
) ([]gamestate.Card, []gamestate.Deck, map[int8]gamestate.Card, error) {
	cards := make([]gamestate.Card, 0, len(seed.Cards))
	decks := make([]gamestate.Deck, 0, systemPartnerArthurCount)
	leaders := make(map[int8]gamestate.Card, systemPartnerArthurCount)
	seenCards := make(map[int64]struct{}, len(seed.Cards))
	for arthurType := int8(1); arthurType <= systemPartnerArthurCount; arthurType++ {
		loadoutCards, deck, leader, err := SystemPartnerLoadoutFromSeed(seed, arthurType)
		if err != nil {
			return nil, nil, nil, err
		}
		for _, card := range loadoutCards {
			if _, exists := seenCards[card.UniqueID]; exists {
				continue
			}
			cards = append(cards, card)
			seenCards[card.UniqueID] = struct{}{}
		}
		decks = append(decks, deck)
		leaders[arthurType] = leader
	}
	return cards, decks, leaders, nil
}

func (accounts *Accounts) ensureSystemPartnerAccounts(transaction *sql.Tx) error {
	seed, err := LoadSaveState(accounts.storage.seedPath)
	if err != nil {
		return fmt.Errorf("load CN system-partner seed: %w", err)
	}
	partnerCards, partnerDecks, partnerLeaders, err := SystemPartnerPopulationFromSeed(seed)
	if err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	for arthurType := int8(1); arthurType <= systemPartnerArthurCount; arthurType++ {
		userID := SystemPartnerUserID(arthurType)
		if _, err := transaction.ExecContext(
			context.Background(),
			`INSERT INTO cn_local_account
			 (user_id, login_uuid, session_key, created_utc, last_login_utc)
			 VALUES (?, ?, ?, ?, ?)
			 ON CONFLICT(user_id) DO NOTHING`,
			userID,
			fmt.Sprintf("system-partner-%d", arthurType),
			fmt.Sprintf("local-cn-system-partner-%d", arthurType),
			now,
			now,
		); err != nil {
			return fmt.Errorf("create CN system-partner account %d: %w", userID, err)
		}
		state := seed
		if err := InitializeOnboardingSnapshot(&state, userID); err != nil {
			return fmt.Errorf("initialize CN system-partner account %d: %w", userID, err)
		}
		state.Cards = make([]gamestate.Card, len(partnerCards))
		for index, card := range partnerCards {
			card.SkillLevels = append([]int16(nil), card.SkillLevels...)
			state.Cards[index] = card
		}
		state.Decks = make([]gamestate.Deck, len(partnerDecks))
		for index, deck := range partnerDecks {
			state.Decks[index] = cloneReleaseDeck(deck)
		}
		state.SupportDeck.CardCollectionIDs = make([]int, 0, len(state.Cards))
		for _, card := range state.Cards {
			state.SupportDeck.CardCollectionIDs = append(state.SupportDeck.CardCollectionIDs, card.CardID)
		}
		leader := partnerLeaders[arthurType]
		state.User.Name = SystemPartnerNameByArthur[arthurType]
		state.User.Comment = "一起完成最初的一步吧！"
		state.User.ActiveArthurType = int(arthurType)
		state.User.LeaderCardID = leader.CardID
		state.User.LeaderCardUniqueID = leader.UniqueID
		state.User.UnlockedFeatureIDs = append(
			[]uint{uint(arthurType - 1)},
			state.User.UnlockedFeatureIDs...,
		)
		state.User.TutorialFlag = (int64(1) << gamestate.TutorialFlagCount) - 1
		state.Onboarding = gamestate.OnboardingState{
			Step: gamestate.OnboardingStepCount,
		}
		content, err := EncodeAccountMetadata(state)
		if err != nil {
			return fmt.Errorf("encode CN system-partner account %d: %w", userID, err)
		}
		if err := writeSnapshotRows(transaction, state, content); err != nil {
			return err
		}
		digest := sha256.Sum256(content)
		if _, err := transaction.ExecContext(
			context.Background(),
			`INSERT INTO cn_account_snapshot
			 (user_id, schema_version, revision, updated_utc, payload_json, payload_sha256)
			 VALUES (?, ?, 1, ?, ?, ?)
			 ON CONFLICT(user_id) DO UPDATE SET
			  schema_version = excluded.schema_version,
			  revision = cn_account_snapshot.revision + 1,
			  updated_utc = excluded.updated_utc,
			  payload_json = excluded.payload_json,
			  payload_sha256 = excluded.payload_sha256
			 WHERE cn_account_snapshot.payload_sha256 <> excluded.payload_sha256`,
			userID,
			saveSnapshotSchemaVersion,
			now,
			content,
			hex.EncodeToString(digest[:]),
		); err != nil {
			return fmt.Errorf("create CN system-partner snapshot %d: %w", userID, err)
		}
		var snapshotRevision int
		var snapshotUpdatedUTC string
		if err := transaction.QueryRowContext(
			context.Background(),
			`SELECT revision, updated_utc FROM cn_account_snapshot WHERE user_id = ?`,
			userID,
		).Scan(&snapshotRevision, &snapshotUpdatedUTC); err != nil {
			return fmt.Errorf("read CN system-partner snapshot metadata %d: %w", userID, err)
		}
		if err := upsertAccountProjection(
			transaction,
			userID,
			snapshotRevision,
			snapshotUpdatedUTC,
			state,
		); err != nil {
			return fmt.Errorf("project CN system-partner account %d: %w", userID, err)
		}
	}
	return nil
}

// ResolveInternalLogin is for trusted fixtures and internal provisioning only.
// Client routes must use ResolveClientLogin with a validated network source.
func (accounts *Accounts) ResolveInternalLogin(loginUUID string) (accountIdentity, error) {
	return accounts.resolveLogin(context.Background(), loginUUID, nil)
}

func (accounts *Accounts) resolveLogin(ctx context.Context, loginUUID string, admission *creationAdmission) (accountIdentity, error) {
	if err := ctx.Err(); err != nil {
		return accountIdentity{}, err
	}
	loginUUID = strings.ToLower(strings.TrimSpace(loginUUID))
	if !validLoginUUID(loginUUID) {
		return accountIdentity{}, errors.New("CN login UUID is invalid")
	}
	if admission != nil {
		if err := accounts.precheckCreation(ctx, loginUUID, admission); err != nil {
			return accountIdentity{}, err
		}
	}
	accounts.mu.Lock()
	defer accounts.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return accountIdentity{}, err
	}

	database, err := accounts.storage.Open()
	if err != nil {
		return accountIdentity{}, err
	}
	transaction, err := database.BeginTx(ctx, &sql.TxOptions{})
	if err != nil {
		return accountIdentity{}, fmt.Errorf("begin CN local login: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = transaction.Rollback()
		}
	}()

	identity, err := accounts.resolveLoginTransaction(ctx, transaction, loginUUID, true, admission)
	if err != nil {
		return accountIdentity{}, err
	}
	if err := ctx.Err(); err != nil {
		return accountIdentity{}, err
	}
	if err := transaction.Commit(); err != nil {
		return accountIdentity{}, fmt.Errorf("commit CN local login: %w", err)
	}
	committed = true
	return identity, nil
}

// Caller owns accounts.mu and the transaction. Binding does not rotate an active game session.
func (accounts *Accounts) resolveLoginTransaction(ctx context.Context, transaction *sql.Tx, loginUUID string, rotate bool, admission *creationAdmission) (accountIdentity, error) {
	var identity accountIdentity
	var blockUntil sql.NullInt64
	err := transaction.QueryRowContext(
		ctx,
		`SELECT user_id, login_uuid, session_key, login_block_until_unix
		 FROM cn_local_account WHERE login_uuid = ?`,
		loginUUID,
	).Scan(&identity.UserID, &identity.LoginUUID, &identity.SessionKey, &blockUntil)
	clock := time.Now().UTC()
	now := clock.Format(time.RFC3339Nano)
	if err == nil && !rotate {
		return identity, nil
	}
	if err == nil {
		if loginBlockActive(blockUntil, clock.Unix()) {
			return accountIdentity{}, &LoginBlockedError{UntilUnix: blockUntil.Int64}
		}
		if admission != nil && !admission.guard.policy().LoginWhitelist.Allows(identity.UserID) {
			return accountIdentity{}, ErrLoginWhitelist
		}
		sessionKey, err := newSessionKey()
		if err != nil {
			return accountIdentity{}, err
		}
		if _, err := transaction.ExecContext(
			ctx,
			`UPDATE cn_local_account SET last_login_utc = ?, session_key = ? WHERE user_id = ?`,
			now,
			sessionKey,
			identity.UserID,
		); err != nil {
			return accountIdentity{}, fmt.Errorf("update CN local login: %w", err)
		}
		identity.SessionKey = sessionKey
	} else if !errors.Is(err, sql.ErrNoRows) {
		return accountIdentity{}, fmt.Errorf("read CN local account: %w", err)
	} else {
		if admission != nil {
			if err := admission.claim(ctx, transaction); err != nil {
				return accountIdentity{}, err
			}
		}
		var nextUserID int
		if err := transaction.QueryRowContext(
			ctx,
			`SELECT COALESCE(MAX(user_id), ?) + 1
			 FROM cn_local_account WHERE user_id < ?`,
			PrimaryUserID-1,
			SystemPartnerUserIDBase,
		).Scan(&nextUserID); err != nil {
			return accountIdentity{}, fmt.Errorf("allocate CN local user ID: %w", err)
		}
		sessionKey, err := newSessionKey()
		if err != nil {
			return accountIdentity{}, err
		}
		identity = accountIdentity{
			UserID:     nextUserID,
			LoginUUID:  loginUUID,
			SessionKey: sessionKey,
		}
		if _, err := transaction.ExecContext(
			ctx,
			`INSERT INTO cn_local_account
			 (user_id, login_uuid, session_key, created_utc, last_login_utc)
			 VALUES (?, ?, ?, ?, ?)`,
			identity.UserID,
			identity.LoginUUID,
			identity.SessionKey,
			now,
			now,
		); err != nil {
			return accountIdentity{}, fmt.Errorf("create CN local account: %w", err)
		}
		if identity.UserID != PrimaryUserID {
			if err := accounts.insertSeedSnapshot(ctx, transaction, identity.UserID, now); err != nil {
				return accountIdentity{}, err
			}
		} else {
			var revision int
			var updatedUTC string
			var content []byte
			var expectedDigest string
			if err := transaction.QueryRowContext(
				ctx,
				`SELECT revision, updated_utc, payload_json, payload_sha256
				 FROM cn_save_snapshot WHERE singleton = 1`,
			).Scan(&revision, &updatedUTC, &content, &expectedDigest); err != nil {
				return accountIdentity{}, fmt.Errorf("read primary CN account snapshot: %w", err)
			}
			digest := sha256.Sum256(content)
			if hex.EncodeToString(digest[:]) != expectedDigest {
				return accountIdentity{}, errors.New("primary CN account snapshot digest mismatch")
			}
			state, err := accounts.storage.decodeAccount(transaction, content)
			if err != nil {
				return accountIdentity{}, err
			}
			if err := upsertAccountProjection(transaction, identity.UserID, revision, updatedUTC, state); err != nil {
				return accountIdentity{}, err
			}
		}
	}
	return identity, nil
}

func (accounts *Accounts) insertSeedSnapshot(ctx context.Context, transaction *sql.Tx, userID int, now string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	state, err := LoadSaveState(accounts.storage.seedPath)
	if err != nil {
		return fmt.Errorf("load CN account seed: %w", err)
	}
	if err := InitializeOnboardingSnapshot(&state, userID); err != nil {
		return err
	}
	content, err := EncodeAccountMetadata(state)
	if err != nil {
		return err
	}
	if err := writeSnapshotRows(transaction, state, content); err != nil {
		return err
	}
	digest := sha256.Sum256(content)
	if _, err := transaction.ExecContext(ctx,
		`INSERT INTO cn_account_snapshot (user_id, schema_version, revision, updated_utc, payload_json, payload_sha256)
         VALUES (?, ?, 1, ?, ?, ?)`, userID, saveSnapshotSchemaVersion, now, content, hex.EncodeToString(digest[:])); err != nil {
		return fmt.Errorf("create CN account snapshot: %w", err)
	}
	return upsertAccountProjection(transaction, userID, 1, now, state)
}

func (accounts *Accounts) LoadState(userID int) (gamestate.State, error) {
	state, err := accounts.LoadPersistentState(userID)
	if err != nil {
		return state, err
	}
	accounts.battleMu.Lock()
	if session := accounts.battles[userID]; session != nil {
		state.ActiveTeamBattle = cloneActiveTeamBattleState(session.state)
		state.StoryTeamBattleSession = session.story
	}
	accounts.battleMu.Unlock()
	return state, nil
}

func (accounts *Accounts) LoadPersistentState(userID int) (gamestate.State, error) {
	if userID == PrimaryUserID {
		return accounts.storage.LoadOrImport()
	}
	if userID < PrimaryUserID {
		return gamestate.State{}, errors.New("CN local user ID is invalid")
	}
	database, err := accounts.storage.OpenRead()
	if err != nil {
		return gamestate.State{}, err
	}
	transaction, err := database.BeginTx(context.Background(), &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return gamestate.State{}, err
	}
	defer transaction.Rollback()
	var schemaVersion int
	var content []byte
	var expectedDigest string
	if err := transaction.QueryRowContext(context.Background(),
		`SELECT schema_version, payload_json, payload_sha256 FROM cn_account_snapshot WHERE user_id = ?`,
		userID).Scan(&schemaVersion, &content, &expectedDigest); err != nil {
		return gamestate.State{}, fmt.Errorf("read CN account snapshot: %w", err)
	}
	if schemaVersion != saveSnapshotSchemaVersion {
		return gamestate.State{}, fmt.Errorf("unsupported CN account snapshot schema %d; use a fresh database", schemaVersion)
	}
	digest := sha256.Sum256(content)
	if hex.EncodeToString(digest[:]) != expectedDigest {
		return gamestate.State{}, errors.New("CN account snapshot digest mismatch")
	}
	state, err := accounts.storage.decodeAccount(transaction, content)
	if err != nil {
		return gamestate.State{}, err
	}
	if state.User.UserID != userID {
		return gamestate.State{}, errors.New("CN account snapshot user ID mismatch")
	}
	if err := transaction.Commit(); err != nil {
		return gamestate.State{}, err
	}
	return state, nil
}

func (accounts *Accounts) PersistState(userID int, state gamestate.State) error {
	return accounts.PersistStateWithAudit(userID, state, nil)
}

func (accounts *Accounts) PersistStateWithAudit(userID int, state gamestate.State, audit *AdminAudit) error {
	if err := accounts.persistAccountData(userID, state, audit); err != nil {
		return err
	}
	// Commit currency/reward changes first. Failed persistence must retain the
	// prior runtime context when the account handler is discarded and reloaded.
	accounts.RememberBattle(userID, state.ActiveTeamBattle, state.StoryTeamBattleSession)
	return nil
}

func (accounts *Accounts) persistAccountData(userID int, state gamestate.State, audit *AdminAudit) error {
	if state.User.UserID != userID {
		return errors.New("refusing to persist a CN account under a different user ID")
	}
	if userID == PrimaryUserID {
		return accounts.storage.persistWithAudit(state, audit)
	}
	content, err := EncodeAccountMetadata(state)
	if err != nil {
		return err
	}
	database, err := accounts.storage.Open()
	if err != nil {
		return err
	}
	transaction, err := database.BeginTx(context.Background(), &sql.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin CN account snapshot update: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = transaction.Rollback()
		}
	}()
	digest := sha256.Sum256(content)
	updatedUTC := time.Now().UTC().Format(time.RFC3339Nano)
	if err := writeAccountSnapshot(transaction, userID, state, content, hex.EncodeToString(digest[:]), updatedUTC, audit); err != nil {
		return err
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit CN account snapshot update: %w", err)
	}
	committed = true
	return nil
}

func writeAccountSnapshot(transaction *sql.Tx, userID int, state gamestate.State, content []byte, digestText, updatedUTC string, audit *AdminAudit) error {
	if err := writeSnapshotRows(transaction, state, content); err != nil {
		return err
	}
	result, err := transaction.ExecContext(
		context.Background(),
		`UPDATE cn_account_snapshot
		 SET schema_version = ?, revision = revision + 1, updated_utc = ?,
		     payload_json = ?, payload_sha256 = ?
		 WHERE user_id = ?`,
		saveSnapshotSchemaVersion,
		updatedUTC,
		content,
		digestText,
		userID,
	)
	if err != nil {
		return fmt.Errorf("write CN account snapshot: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil || rows != 1 {
		return errors.New("CN account snapshot update did not affect exactly one row")
	}
	var revision int
	if err := transaction.QueryRowContext(
		context.Background(),
		`SELECT revision FROM cn_account_snapshot WHERE user_id = ?`,
		userID,
	).Scan(&revision); err != nil {
		return fmt.Errorf("read updated CN account snapshot revision: %w", err)
	}
	if err := upsertAccountProjection(transaction, userID, revision, updatedUTC, state); err != nil {
		return err
	}
	return audit.appendTo(transaction, updatedUTC)
}

// ListPVPOpponents returns persisted local accounts other than the challenger.
// PVP is asynchronous: the selected account does not need an active process or
// an in-memory multiplayer room.
func (accounts *Accounts) ListPVPOpponents(userID int) ([]gamestate.State, error) {
	return accounts.ListLocalAccountStates(userID, false)
}

func (accounts *Accounts) ListLocalAccountStates(
	userID int,
	includeSystemPartners bool,
) ([]gamestate.State, error) {
	database, err := accounts.storage.OpenRead()
	if err != nil {
		return nil, err
	}
	query := `SELECT account.user_id, projection.payload_json, projection.payload_sha256, account.last_login_utc
		FROM cn_local_account AS account
		JOIN cn_account_projection AS projection ON projection.user_id = account.user_id
		WHERE account.user_id <> ? AND account.user_id < ? ORDER BY account.user_id`
	arguments := []any{userID, SystemPartnerUserIDBase}
	if includeSystemPartners {
		query = `SELECT account.user_id, projection.payload_json, projection.payload_sha256, account.last_login_utc
			FROM cn_local_account AS account
			JOIN cn_account_projection AS projection ON projection.user_id = account.user_id
			WHERE account.user_id <> ? ORDER BY account.user_id`
		arguments = []any{userID}
	}
	rows, err := database.QueryContext(context.Background(), query, arguments...)
	if err != nil {
		return nil, fmt.Errorf("list CN local PVP opponents: %w", err)
	}
	defer rows.Close()
	result := make([]gamestate.State, 0)
	for rows.Next() {
		var opponentUserID int
		var content []byte
		var expectedDigest string
		var lastLoginUTC string
		if err := rows.Scan(&opponentUserID, &content, &expectedDigest, &lastLoginUTC); err != nil {
			return nil, fmt.Errorf("scan CN local PVP opponent: %w", err)
		}
		state, err := DecodeAccountProjection(content, expectedDigest)
		if err != nil {
			return nil, fmt.Errorf("decode CN local account projection %d: %w", opponentUserID, err)
		}
		if state.User.UserID != opponentUserID {
			return nil, fmt.Errorf("CN local account projection %d has a mismatched user ID", opponentUserID)
		}
		lastLogin, err := time.Parse(time.RFC3339Nano, lastLoginUTC)
		if err != nil {
			return nil, fmt.Errorf("decode CN account %d login time: %w", opponentUserID, err)
		}
		state.LastLoginUnix = lastLogin.Unix()
		// System partners are always available local helpers, without player sessions.
		if isSystemPartnerUserID(opponentUserID) {
			state.LastLoginUnix = time.Now().Unix()
		}
		result = append(result, state)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate CN local PVP opponents: %w", err)
	}
	return result, nil
}

func validLoginUUID(value string) bool {
	if len(value) != 36 || value[8] != '-' || value[13] != '-' || value[18] != '-' || value[23] != '-' {
		return false
	}
	for index, character := range value {
		if index == 8 || index == 13 || index == 18 || index == 23 {
			continue
		}
		if (character < '0' || character > '9') && (character < 'a' || character > 'f') {
			return false
		}
	}
	return true
}

func newSessionKey() (string, error) {
	var entropy [32]byte
	if _, err := rand.Read(entropy[:]); err != nil {
		return "", fmt.Errorf("generate CN account session: %w", err)
	}
	return "local-cn-" + hex.EncodeToString(entropy[:]), nil
}

func (accounts *Accounts) ResolveSession(sessionKey string) (int, error) {
	return accounts.ResolveSessionContext(context.Background(), sessionKey)
}

func (accounts *Accounts) ResolveSessionContext(ctx context.Context, sessionKey string) (int, error) {
	if len(sessionKey) < 32 || len(sessionKey) > 128 {
		return 0, ErrInvalidSession
	}
	database, err := accounts.storage.OpenRead()
	if err != nil {
		return 0, err
	}
	var userID int
	if err := database.QueryRowContext(
		ctx,
		`SELECT user_id FROM cn_local_account WHERE session_key = ?`,
		sessionKey,
	).Scan(&userID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, ErrInvalidSession
		}
		return 0, fmt.Errorf("resolve CN account session: %w", err)
	}
	if userID < PrimaryUserID || isSystemPartnerUserID(userID) {
		return 0, ErrInvalidSession
	}
	return userID, nil
}
