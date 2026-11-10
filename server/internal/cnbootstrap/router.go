package cnbootstrap

import (
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"

	"github.com/go-chi/chi/v5"

	"kairisei.local/server/internal/accounthttp"
	"kairisei.local/server/internal/accountstore"
	adminapi "kairisei.local/server/internal/admin"
	"kairisei.local/server/internal/game"
	"kairisei.local/server/internal/gamestate"
	"kairisei.local/server/internal/operations"
)

// application wires long-lived services. Protocol handlers never construct
// databases or reload resource masters while registering their routes.
type application struct {
	config      Config
	resources   *clientResources
	accounts    *accountstore.Accounts
	operations  *operations.Runtime
	editor      *adminapi.Operations
	business    *accounthttp.Router
	recorder    *recorder
	progression gamestate.PlayerProgressionPolicy
	pvpRanks    []game.PVPRankConfig
}

func (app *application) router() chi.Router {
	router := chi.NewRouter()
	router.Use(app.blockIPRequests)
	router.Use(cnNetworkCompression(app.config.Logger))
	router.Use(app.recorder.middleware(app.config.Logger))
	router.Use(normalizeLeadingSlashes)
	router.Use(app.maintenanceRequests)
	router.Use(authenticateCNSessions(app.accounts))
	router.Use(markAuthenticatedCapture)
	router.Get("/healthz", func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(writer).Encode(map[string]any{
			"state":        "PASS",
			"battle_rooms": app.config.Multiplayer.RoomCount(),
			"battle_port":  app.config.Network.BattleSV.Port,
		})
	})
	for _, name := range []string{"default", "apple-review", "qa"} {
		router.Get("/local/server/"+name+".list", cnBootstrapServerList(app.config.Network.AdvertiseHost, app.config.Network.HTTPPort))
	}
	for key, bannerPath := range app.resources.Banners {
		route := "/local/gacha/" + key + ".png"
		switch key {
		case "":
			route = "/local/gacha/banner.png"
		case "five_star_ticket":
			route = "/local/gacha/five-star-banner.png"
		case "home":
			route = "/local/home/banner.png"
		}
		router.Get(route, func(writer http.ResponseWriter, request *http.Request) {
			writer.Header().Set("Content-Type", "image/png")
			writer.Header().Set("Cache-Control", "no-store")
			http.ServeFile(writer, request, bannerPath)
		})
	}
	// Operator-uploaded pool covers and home banners (content-addressed files next to the save database).
	router.Get("/local/gacha-covers/{file}", adminapi.ServeGachaCover(app.gachaCoverDir()))
	router.Get("/local/home-banners/{file}", adminapi.ServeHomeBanner(app.homeBannerDir()))
	emptyJSON := func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json; charset=utf-8")
		writer.Header().Set("Cache-Control", "no-store")
		_, _ = writer.Write([]byte("{}"))
	}
	emptyBody := func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "text/plain; charset=utf-8")
		writer.Header().Set("Cache-Control", "no-store")
		writer.WriteHeader(http.StatusOK)
	}
	router.Get("/xxxx/g0/apk.config", emptyJSON)
	router.Get("/xxxx/g0/dex.config", emptyJSON)
	router.Get("/xxxx/feature/*", emptyBody)
	router.Get("/xxxx/html/latest_default.json", emptyBody)
	router.Post("/disabled/envsdk/optsdk/*", emptyJSON)
	// Fixed-width DEX redirects for longer RFC1918 hosts omit the emulator-only
	// prefixes above. Keep both representations at the HTTP boundary.
	router.Get("/g0/apk.config", emptyJSON)
	router.Get("/g0/dex.config", emptyJSON)
	router.Get("/feature/*", emptyBody)
	router.Get("/html/latest_default.json", emptyBody)
	router.Post("/d/*", emptyJSON)
	router.Post("/log.php", acknowledgeClientLog)
	router.Post("/mods_switch.php", app.moduleSwitches)
	modernLogin := platformClientLogin(app.accounts, app.config.Network.AdvertiseHost, app.config.Network.HTTPPort, app.config.CDN, app.resources.Images.Namespace, app.resources.Platforms)
	router.Post("/local/client/login", modernLogin)
	accountGateway := newCNAccountGateway(app.accounts)
	for _, action := range []string{"status", "bind", "login", "change-password"} {
		router.Post("/local/account/"+action, accountGateway.ServeHTTP)
	}
	router.Post("/subcribe_push.php", cnBootstrapPushRegistration)
	app.registerGameRoutes(router)
	router.Get("/disabled/products", cnBootstrapProducts)
	router.Get("/disabled/web", app.editor.LocalNotice)
	router.Get("/disabled/web/deck-guide", cnBootstrapDeckGuide)
	router.Get("/disabled/web/information/2015/7/kechengbiao", cnBootstrapDungeonSchedule)
	introHandler, err := newCNIntroHandler(app.resources.Banners[""])
	if err != nil {
		app.config.Logger.Warn("local download guide unavailable", "error", err)
		introHandler = cnBootstrapIntroPlaceholder
	}
	router.Get("/disabled/web/netease/fourplusone/20160626/Intro_{index}.png", introHandler)
	registerPlatformContent(router, app.resources.Platforms, app.resources.CPKList, app.resources.CPKState)
	cpkResource := cnBootstrapCPKResource(app.resources.CPK)
	router.Get("/local/resources/cpk/*", cpkResource)
	router.Head("/local/resources/cpk/*", cpkResource)
	router.Get("/local/resources/image/*", app.resources.Images.serveHTTP)
	router.Head("/local/resources/image/*", app.resources.Images.serveHTTP)
	router.NotFound(func(writer http.ResponseWriter, _ *http.Request) {
		http.Error(writer, "CN route adapter not implemented", http.StatusNotImplemented)
	})
	return router
}

func (app *application) maintenanceRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet || r.Method == http.MethodHead {
			next.ServeHTTP(w, r)
			return
		}
		done, ok := app.operations.AdmitGameRequest()
		if !ok {
			w.Header().Set("Retry-After", "60")
			if businessCompressionRoute(r) {
				common := cnBootstrapCommon()
				common["res_code"] = -1
				common["res_str"] = "服务器维护中，请稍后再试"
				common["res_err_action"] = 1
				writeCNProtocolResponseWithPopup(w, common, map[string]any{})
				return
			}
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusServiceUnavailable)
			_ = json.NewEncoder(w).Encode(map[string]any{"res_code": 503, "res_str": "服务器维护中，请稍后再试"})
			return
		}
		defer done()
		next.ServeHTTP(w, r)
	})
}

func (app *application) handler() (*cnDeploymentHandler, error) {
	if err := app.accounts.ValidateCreationPolicy(); err != nil {
		return nil, err
	}
	adminHandler, err := adminapi.New(adminapi.Config{
		Accounts: app.accounts, Runtime: app.business, Operations: app.editor,
		BattleMaster: app.config.Masters.Battle, CardMaster: app.config.Masters.Cards, ItemMaster: app.config.Masters.Items,
		PVPRanks:    app.pvpRanks,
		Multiplayer: app.config.Multiplayer, AdvertiseHost: app.config.Network.AdvertiseHost, GamePort: app.config.Network.HTTPPort,
		Logger: app.config.Logger, Progression: app.progression,
		NetworkMetrics: app.config.NetworkMetrics,
		RuntimeLogPath: app.config.AdminLogFile,
		GachaBanners:   app.resources.gachaBanners(), AssetMap: app.config.Resources.AssetMap,
		GachaCoverDir: app.gachaCoverDir(), HomeBannerDir: app.homeBannerDir(), ImageStorage: app.config.ImageStorage,
	})
	if err != nil {
		return nil, fmt.Errorf("initialize CN admin handler: %w", err)
	}
	return &cnDeploymentHandler{Handler: app.router(), admin: adminHandler, database: app.accounts.Database(), accounts: app.accounts, operations: app.operations}, nil
}

// gachaCoverDir is the fixed, writable cover folder: "gacha-covers" beside the save database.
func (app *application) gachaCoverDir() string {
	return filepath.Join(filepath.Dir(app.config.Persistence.SavePath), "gacha-covers")
}

// homeBannerDir is the fixed, writable home banner folder: "home-banners" beside the save database.
func (app *application) homeBannerDir() string {
	return filepath.Join(filepath.Dir(app.config.Persistence.SavePath), "home-banners")
}
