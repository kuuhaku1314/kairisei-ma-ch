package httpapi

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"kairisei.local/server/battlemodel"
	"kairisei.local/server/internal/clientbridge"
	"kairisei.local/server/internal/game"
	"kairisei.local/server/internal/gamestate"
	"kairisei.local/server/internal/multiplayer"
	"kairisei.local/server/internal/operations"
)

type API struct {
	popupAnnouncements  []game.PopupAnnouncement
	homeBanners         operations.HomeBannerConfiguration
	giftCodes           game.GiftCodeRepository
	requestRejected     bool // Protected by the account coordinator, like all per-request mutations.
	initialState        gamestate.State
	account             *game.Account
	baseURL             string
	gachaBannerVersions map[string]string
	cardShowProjection  func(CardShowView) (any, error)
	cardParameters      *clientbridge.CardParameterCatalog
	combatDefinitions   *battlemodel.Definitions

	logger              *slog.Logger
	persistState        game.StatePersister
	multiplayer         *multiplayer.Hub
	battleSV            multiplayer.Endpoint
	pvpAccounts         game.PVPAccountRepository
	pvpConfig           game.PVPConfig
	friendPointAccounts game.FriendPointAccountRepository
	battleHistory       game.BattleHistoryRepository
	bossScoreRankings   game.BossScoreRankingRepository
	towerRankings       game.TowerRankingRepository
	teamBattleResultMu  sync.Mutex
	clientResultMu      sync.Mutex
}

// Config is the dependency boundary for one account's business handler.
// Tests may omit persistence and multiplayer dependencies for isolated account operations.
type Config struct {
	GiftCodes           game.GiftCodeRepository
	MissionCatalog      *game.MissionCatalog
	BattleCatalog       *game.TeamBattleDefinitions
	CardShowProjection  func(CardShowView) (any, error)
	InitialState        gamestate.State
	BaseURL             string
	GachaBannerVersions map[string]string
	Logger              *slog.Logger
	PersistState        game.StatePersister
	Multiplayer         *multiplayer.Hub
	BattleSV            multiplayer.Endpoint
	PVPAccounts         game.PVPAccountRepository
	PVP                 game.PVPConfig
	FriendPointAccounts game.FriendPointAccountRepository
	BattleHistory       game.BattleHistoryRepository
	BossScoreRankings   game.BossScoreRankingRepository
	TowerRankings       game.TowerRankingRepository
	CombatDefinitions   *battlemodel.Definitions
	CardCatalog         *game.CardCatalog
}

func New(config Config) (http.Handler, error) {
	if config.InitialState.User.UserID <= 0 {
		return nil, errors.New("initial account state is required")
	}
	baseURL := strings.TrimRight(config.BaseURL, "/")
	if baseURL == "" {
		return nil, errors.New("advertised base URL is required")
	}
	logger := config.Logger
	if logger == nil {
		logger = slog.Default()
	}
	if config.CardCatalog != nil {
		config.CardCatalog.ApplyDefinitions(&config.InitialState)
	}
	cardParameters := clientbridge.NewCardParameterCatalog(config.CombatDefinitions, config.InitialState.CardProgressionPolicy)
	cardStore, err := game.New(config.InitialState,
		game.WithCombatDefinitions(config.CombatDefinitions), game.WithCardCatalog(config.CardCatalog), game.WithBattleCatalog(config.BattleCatalog), game.WithMissionCatalog(config.MissionCatalog), game.WithAlchemistMainParameters(cardParameters))
	if err != nil {
		return nil, err
	}
	cardStore.SetMissionPVPRanks(config.PVP.Ranks)
	api := &API{
		cardShowProjection:  config.CardShowProjection,
		initialState:        accountSnapshotBase(config.InitialState),
		account:             cardStore,
		baseURL:             baseURL,
		gachaBannerVersions: config.GachaBannerVersions,
		cardParameters:      cardParameters,
		combatDefinitions:   config.CombatDefinitions,
		logger:              logger,
		persistState:        config.PersistState,
		multiplayer:         config.Multiplayer,
		battleSV:            config.BattleSV,
		pvpAccounts:         config.PVPAccounts,
		pvpConfig:           config.PVP,
		friendPointAccounts: config.FriendPointAccounts,
		battleHistory:       config.BattleHistory,
		bossScoreRankings:   config.BossScoreRankings,
		towerRankings:       config.TowerRankings,
		giftCodes:           config.GiftCodes,
	}
	if cardStore.RequiresInitialSave() && config.PersistState != nil {
		if err := config.PersistState(cardStore.Snapshot(config.InitialState)); err != nil {
			return nil, fmt.Errorf("persist reconciled initial state: %w", err)
		}
	}
	router := chi.NewRouter()
	router.Use(middleware.RequestID)
	router.Use(middleware.Recoverer)
	router.Use(api.logRequest)
	api.register(router)
	return &accountBusinessHandler{Handler: router, api: api}, nil
}

// These private collections are reconstructed by Account.Snapshot. Retaining
// their load-time copies would keep sold cards and claimed mail alive until
// this handler is evicted. Identity and protocol/master context remain intact.
func accountSnapshotBase(state gamestate.State) gamestate.State {
	state.Cards = nil
	state.ContainerCards = nil
	state.Decks = nil
	state.StackCards = nil
	state.Spheres = nil
	state.Buddies = nil
	state.Engagement.Presents = nil
	state.Engagement.Histories = nil
	state.TeamBattleSolo = nil
	return state
}
