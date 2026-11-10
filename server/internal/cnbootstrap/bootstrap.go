package cnbootstrap

import (
	"errors"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"

	"kairisei.local/server/internal/accounthttp"
	"kairisei.local/server/internal/accountstore"
	adminapi "kairisei.local/server/internal/admin"
	"kairisei.local/server/internal/game"
	"kairisei.local/server/internal/gamestate"
	"kairisei.local/server/internal/httpapi"
	"kairisei.local/server/internal/masterdata"
)

const (
	cn602MenuBundle   = "main_c/scenes/scene_Menu.dat"
	cn602LocalSession = "local-cn-session-key"
)

var cn602ScrambleKey = [...]byte{0x01, 0xcd, 0x45, 0x89, 0x67, 0xab, 0x23, 0xef}

// New assembles the CN protocol adapter, account runtime and resource service.
// The owner must close the returned handler after all
// HTTP/Admin/BattleSv work has drained, including on listener setup failure.
func New(config Config) (*cnDeploymentHandler, error) {
	app, err := assembleApplication(config)
	if err != nil {
		return nil, err
	}
	handler, err := app.handler()
	if err != nil {
		_ = app.accounts.Database().Close()
		return nil, err
	}
	return handler, nil
}

func assembleApplication(config Config) (*application, error) {
	if config.Logger == nil {
		config.Logger = slog.Default()
	}
	cdn, cdnErr := config.CDN.normalized()
	if cdnErr != nil {
		return nil, cdnErr
	}
	config.CDN = cdn
	if config.Multiplayer == nil || config.Network.BattleSV.Host == "" || config.Network.BattleSV.Port == 0 {
		return nil, errors.New("local multiplayer runtime is required")
	}
	if config.Persistence.RequestLog == "" {
		return nil, errors.New("request log path is required")
	}
	if config.Network.AdvertiseHost == "" {
		return nil, errors.New("advertise host is required")
	}
	if config.Network.HTTPPort < 1 || config.Network.HTTPPort > 65535 {
		return nil, errors.New("port must be 1 through 65535")
	}
	resources, err := loadClientResources(config.Resources)
	if err != nil {
		return nil, err
	}
	saveDatabase, err := accountstore.OpenDatabase(config.Persistence.SavePath, config.Persistence.SeedPath, config.Logger)
	if err != nil {
		return nil, err
	}
	assembled := false
	defer func() {
		if !assembled {
			_ = saveDatabase.Close()
		}
	}()
	pvpConfig, err := masterdata.LoadPVPRuntimeMaster(config.Masters.PVP)
	if err != nil {
		return nil, err
	}
	playerProgression, err := masterdata.LoadPlayerProgressionRuntimeMaster(config.Masters.PlayerProgression)
	if err != nil {
		return nil, err
	}
	loginBonusPolicy, err := masterdata.LoadLoginBonusRuntimeMaster(config.Masters.LoginBonus)
	if err != nil {
		return nil, err
	}
	catalog, err := accountstore.LoadSaveState(config.Persistence.SeedPath)
	if err != nil {
		return nil, err
	}
	prepareRuntimeState, err := loadCNRuntimeStatePreparer(config.Masters, playerProgression, loginBonusPolicy, resources.AvailableBundles)
	if err != nil {
		return nil, err
	}
	catalog, err = prepareRuntimeState.prepareCatalog(catalog)
	if err != nil {
		return nil, err
	}
	app, err := assembleAccountServices(config, resources, saveDatabase, catalog,
		prepareRuntimeState.prepareAccount, playerProgression, pvpConfig)
	if err != nil {
		return nil, err
	}
	assembled = true
	return app, nil
}

// assembleAccountServices connects already loaded definitions and account services.
// The caller owns the database and closes it if assembly fails.
func assembleAccountServices(
	config Config,
	resources *clientResources,
	saveDatabase *accountstore.Database,
	catalog gamestate.State,
	prepareAccount func(gamestate.State) (gamestate.State, error),
	playerProgression gamestate.PlayerProgressionPolicy,
	pvpConfig game.PVPConfig,
) (*application, error) {
	cardCatalog, err := game.NewCardCatalog(catalog)
	if err != nil {
		return nil, err
	}
	cardCatalog.ApplyDefinitions(&catalog)
	saveDatabase.SetCatalog(catalog)
	primaryState, err := saveDatabase.LoadOrImport()
	if err != nil {
		return nil, err
	}
	accountStore, err := accountstore.NewAccounts(saveDatabase)
	if err != nil {
		return nil, err
	}
	if err := config.Multiplayer.AttachCompletionRepository(accountStore); err != nil {
		return nil, err
	}
	operationStore, err := adminapi.NewOperations(saveDatabase, primaryState.Gachas)
	if err != nil {
		return nil, err
	}
	if err := accountStore.AttachCreationPolicy(operationStore.AccountCreationPolicy); err != nil {
		return nil, err
	}
	if err := config.Multiplayer.AttachGameSpeed(operationStore.TeamBattleSpeed); err != nil {
		return nil, err
	}
	if err := operationStore.InitializeContent(catalog, config.Masters.Battle); err != nil {
		return nil, err
	}
	if err := operationStore.InitializePlayerPolicy(catalog, config.Masters.Navi); err != nil {
		return nil, err
	}
	buildBusinessHandler := func(userID int, state gamestate.State) (http.Handler, error) {
		content := operationStore.ContentConfiguration()
		prepare := func(state gamestate.State) (gamestate.State, error) {
			prepared, err := prepareAccount(state)
			if err != nil {
				return prepared, err
			}
			return game.ApplyContentState(prepared, content), nil
		}
		return newCNBusinessHandler(httpapi.Config{
			InitialState:        state,
			BaseURL:             "http://" + config.Network.AdvertiseHost + ":" + strconv.Itoa(config.Network.HTTPPort),
			GachaBannerVersions: resources.GachaBannerVersions,
			Logger:              config.Logger,
			PersistState:        func(next gamestate.State) error { return accountStore.PersistState(userID, next) },
			PVP:                 pvpConfig,
			PVPAccounts:         accountStore,
			GiftCodes:           accountStore,
			FriendPointAccounts: accountStore,
			BattleHistory:       accountStore,
			BossScoreRankings:   accountStore,
			TowerRankings:       accountStore,
			BattleSV:            config.Network.BattleSV,
			Multiplayer:         config.Multiplayer,
			CombatDefinitions:   config.CombatDefinitions,
			CardCatalog:         cardCatalog,
			MissionCatalog:      operationStore.MissionCatalog(),
			BattleCatalog:       content.BattleCatalog,
		}, prepare)
	}
	cacheLimit, err := accounthttp.ConfiguredAccountCacheLimit()
	if err != nil {
		return nil, err
	}
	primaryBusinessHandler, err := buildBusinessHandler(accountstore.PrimaryUserID, primaryState)
	if err != nil {
		return nil, err
	}
	businessHandler := accounthttp.New(accounthttp.Config{
		Primary:          primaryBusinessHandler,
		IdleLimit:        cacheLimit,
		Prepare:          func(handler http.Handler) { operationStore.Runtime.PrepareBusiness(handler) },
		PersistStates:    accountStore.PersistStates,
		PersistCompleted: accountStore.PersistCompletedBattle,
		Build: func(userID int) (http.Handler, error) {
			state, err := accountStore.LoadState(userID)
			if err != nil {
				config.Logger.Error("load CN local account snapshot", "user_id", userID, "error", err)
				return nil, err
			}
			handler, err := buildBusinessHandler(userID, state)
			if err != nil {
				config.Logger.Error("build CN local account handler", "user_id", userID, "error", err)
				return nil, err
			}
			return handler, nil
		},
	})
	if err := config.Multiplayer.AttachStartAuthorizer(businessHandler.ChargeMultiplayerStart); err != nil {
		return nil, err
	}
	if err := config.Multiplayer.AttachContinueAuthorizer(businessHandler.ChargeMultiplayerContinue); err != nil {
		return nil, err
	}
	if err := config.Multiplayer.AttachBattleCompleter(businessHandler.CompleteMultiplayerBattle); err != nil {
		return nil, err
	}
	absolute, err := filepath.Abs(config.Persistence.RequestLog)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(absolute)
	if err != nil {
		return nil, err
	}
	if info.IsDir() {
		return nil, errors.New("request log path is a directory")
	}
	r := &recorder{path: absolute}
	app := &application{
		config: config, resources: resources, accounts: accountStore,
		operations: operationStore.Runtime, editor: operationStore, business: businessHandler, recorder: r, progression: playerProgression,
		pvpRanks: pvpConfig.Ranks,
	}
	return app, nil
}
