package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"kairisei.local/server/internal/cdnsync"
	"kairisei.local/server/internal/cnbootstrap"
	"kairisei.local/server/internal/multiplayer"
)

const serverVersion = "1.3.8"

type options struct {
	showVersion       bool
	cdnSyncConfig     string
	cdnSyncDryRun     bool
	cdnConfig         string
	cdnManifestOutput string
	resourceSet       string
	clientProfile     string

	requestLog                string
	cnSavePath                string
	cnSaveSeedPath            string
	cnAssetMapPath            string
	cnCardMasterPath          string
	cnExploreMasterPath       string
	cnStoryMasterPath         string
	cnBattleMasterPath        string
	cnCombatCardPath          string
	cnCombatMasterRoot        string
	cnNaviMasterPath          string
	cnItemMasterPath          string
	cnAvatarMasterPath        string
	cnGachaBannerPath         string
	cnFiveStarGachaBannerPath string
	cnHomeBannerPath          string
	cnStampMasterPath         string
	cnHonorMasterPath         string
	cnPVPMasterPath           string
	cnPlayerProgressionPath   string
	cnLoginBonusPath          string
	cnCPKRoot                 string
	cnImageRoot               string
	cnCPKAliasPath            string
	cnPatchRoots              stringListFlag
	listenHost                string
	advertiseHost             string
	port                      int
	battlePort                int
	adminListenHost           string
	adminPort                 int
	shutdownEvent             string
}

type stringListFlag []string

func (values *stringListFlag) String() string {
	return strings.Join(*values, ",")
}

func (values *stringListFlag) Set(value string) error {
	if value == "" {
		return errors.New("flag value must not be empty")
	}
	*values = append(*values, value)
	return nil
}

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(os.Args[1:], logger); err != nil {
		logger.Error("server failed", "error", err)
		os.Exit(1)
	}
}

func run(arguments []string, logger *slog.Logger) error {
	var opts options
	flags := flag.NewFlagSet("kairi-server", flag.ContinueOnError)
	flags.BoolVar(&opts.showVersion, "version", false, "print server version and exit")
	flags.StringVar(&opts.cdnConfig, "cdn-config", "", "optional public CDN JSON config; empty keeps local downloads")
	flags.StringVar(&opts.cdnManifestOutput, "export-cdn-manifest", "", "export client download object mapping and exit without starting the server")
	flags.StringVar(&opts.cdnSyncConfig, "sync-cdn", "", "sync client resources using this JSON config, then enable CDN and exit; no game service or database")
	flags.BoolVar(&opts.cdnSyncDryRun, "cdn-sync-dry-run", false, "check sync inputs locally without network or writes")
	flags.StringVar(&opts.resourceSet, "resource-set", "", "complete resource-set.json used for CDN export/sync")
	flags.StringVar(&opts.shutdownEvent, "shutdown-event", "", "Windows local graceful shutdown event")
	flags.StringVar(&opts.clientProfile, "client-profile", "", "explicit client profile")

	flags.StringVar(&opts.requestLog, "request-log", "", "CN JSONL request capture")
	flags.StringVar(&opts.cnSavePath, "cn-save-path", "", "editable local CN character save")
	flags.StringVar(&opts.cnSaveSeedPath, "cn-save-seed", "", "versioned local CN save migration seed")
	flags.StringVar(&opts.cnAssetMapPath, "cn-asset-map", "", "generated official CN logical asset map")
	flags.StringVar(&opts.cnCardMasterPath, "cn-card-master", "", "normalized official CN card runtime master")
	flags.StringVar(&opts.cnExploreMasterPath, "cn-explore-master", "", "normalized official CN Explore runtime master")
	flags.StringVar(&opts.cnStoryMasterPath, "cn-story-master", "", "normalized official CN story runtime master")
	flags.StringVar(&opts.cnBattleMasterPath, "cn-battle-master", "", "asset-closed CN TeamBattle local runtime master")
	flags.StringVar(&opts.cnCombatCardPath, "cn-combat-card-master", "", "extracted official CN card CSV used by the Go combat engine")
	flags.StringVar(&opts.cnCombatMasterRoot, "cn-combat-master-root", "", "extracted official CN battle CSV directory used by the Go combat engine")
	flags.StringVar(&opts.cnNaviMasterPath, "cn-navi-master", "", "normalized official CN navigator runtime master")
	flags.StringVar(&opts.cnItemMasterPath, "cn-item-master", "", "normalized official CN item runtime master")
	flags.StringVar(&opts.cnAvatarMasterPath, "cn-avatar-master", "", "normalized official CN Avatar runtime master")
	flags.StringVar(&opts.cnGachaBannerPath, "cn-gacha-banner", "", "read-only official CN cached gacha banner")
	flags.StringVar(&opts.cnFiveStarGachaBannerPath, "cn-five-star-gacha-banner", "", "read-only local-policy five-star gacha banner")
	flags.StringVar(&opts.cnHomeBannerPath, "cn-home-banner", "", "read-only official CN cached Home banner")
	flags.StringVar(&opts.cnStampMasterPath, "cn-stamp-master", "", "normalized official CN stamp runtime master")
	flags.StringVar(&opts.cnHonorMasterPath, "cn-honor-master", "", "normalized official CN honor runtime master")
	flags.StringVar(&opts.cnPVPMasterPath, "cn-pvp-master", "", "CN PVP fields, ranks and local settlement rules")
	flags.StringVar(&opts.cnPlayerProgressionPath, "cn-player-progression", "", "CN player EXP, cap and base-job progression policy")
	flags.StringVar(&opts.cnLoginBonusPath, "cn-login-bonus", "", "CN local daily login bonus policy")
	flags.StringVar(&opts.cnCPKRoot, "cn-cpk-root", "", "read-only official CN CPK root")
	flags.StringVar(&opts.cnImageRoot, "cn-image-root", "", "read-only enlarged card image root")
	flags.StringVar(&opts.cnCPKAliasPath, "cn-cpk-aliases", "", "generated CN CPK logical alias manifest")
	flags.Var(&opts.cnPatchRoots, "cn-patch-root", "read-only official CN patch root; repeat for source overlays")
	flags.StringVar(&opts.listenHost, "listen-host", "0.0.0.0", "local listen address")
	flags.StringVar(&opts.advertiseHost, "advertise-host", "10.0.2.2", "host returned to client")
	flags.IntVar(&opts.port, "port", 26020, "local HTTP port")
	flags.IntVar(&opts.battlePort, "battle-port", 0, "local BattleSv TCP port; defaults to HTTP port plus one")
	flags.StringVar(&opts.adminListenHost, "admin-listen-host", "127.0.0.1", "loopback-only Admin Web listen address")
	flags.IntVar(&opts.adminPort, "admin-port", 0, "loopback-only Admin Web port; zero disables it")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("unexpected positional arguments")
	}
	if opts.showVersion {
		_, err := fmt.Fprintln(os.Stdout, "kairi-server "+serverVersion)
		return err
	}
	if opts.cdnSyncConfig != "" {
		if opts.cdnManifestOutput != "" {
			return errors.New("-sync-cdn and -export-cdn-manifest cannot be combined")
		}
		if opts.resourceSet == "" {
			opts.resourceSet = "resource-set/resource-set.json"
		}
		if opts.cdnConfig == "" {
			opts.cdnConfig = "cdn.json"
		}
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		return cdnsync.Run(ctx, cdnsync.Options{ConfigPath: opts.cdnSyncConfig, ResourceSet: opts.resourceSet, CDNConfigPath: opts.cdnConfig, DryRun: opts.cdnSyncDryRun}, os.Stdout)
	}
	if opts.cdnSyncDryRun {
		return errors.New("-cdn-sync-dry-run requires -sync-cdn")
	}
	if opts.cdnManifestOutput != "" {
		return cnbootstrap.ExportCDNManifest(opts.resourceSet, opts.cdnManifestOutput)
	}
	if opts.resourceSet != "" {
		return errors.New("-resource-set requires -export-cdn-manifest or -sync-cdn")
	}
	cdn, cdnErr := cnbootstrap.LoadCDNConfig(opts.cdnConfig)
	if cdnErr != nil {
		return cdnErr
	}
	if cdn.BaseURL != "" && opts.clientProfile != "cn602-bootstrap" {
		return errors.New("CDN requires cn602-bootstrap")
	}
	if opts.port < 1 || opts.port > 65535 {
		return errors.New("-port must be 1 through 65535")
	}
	if opts.battlePort == 0 {
		if opts.port == 65535 {
			return errors.New("-battle-port is required when HTTP uses port 65535")
		}
		opts.battlePort = opts.port + 1
	}
	if opts.battlePort < 1 || opts.battlePort > 65535 || opts.battlePort == opts.port {
		return errors.New("-battle-port must be 1 through 65535 and differ from -port")
	}
	if opts.adminPort < 0 || opts.adminPort > 65535 {
		return errors.New("-admin-port must be zero or 1 through 65535")
	}
	if opts.adminPort != 0 {
		adminIP := net.ParseIP(opts.adminListenHost)
		if adminIP == nil || !adminIP.IsLoopback() {
			return errors.New("-admin-listen-host must be a loopback IP address")
		}
		if opts.adminPort == opts.port || opts.adminPort == opts.battlePort {
			return errors.New("-admin-port must differ from -port and -battle-port")
		}
	}
	if strings.ContainsAny(opts.advertiseHost, "/?#") || opts.advertiseHost == "" {
		return errors.New("-advertise-host must be a host without URL syntax")
	}

	requested, closeEvent, eventErr := watchShutdownEvent(opts.shutdownEvent)
	if eventErr != nil {
		return eventErr
	}
	defer closeEvent()
	baseURL := "http://" + opts.advertiseHost + ":" + strconv.Itoa(opts.port)
	var handler http.Handler
	var multiplayerHub *multiplayer.Hub
	var battleServer *multiplayer.Server
	var err error
	switch opts.clientProfile {
	case "cn602-bootstrap":
		if opts.cnBattleMasterPath == "" {
			return errors.New("-cn-battle-master is required for cn602-bootstrap")
		}
		if opts.cnCombatCardPath == "" || opts.cnCombatMasterRoot == "" {
			return errors.New("-cn-combat-card-master and -cn-combat-master-root are required for cn602-bootstrap")
		}
		if opts.cnPVPMasterPath == "" {
			return errors.New("-cn-pvp-master is required for cn602-bootstrap")
		}
		if opts.cnPlayerProgressionPath == "" {
			return errors.New("-cn-player-progression is required for cn602-bootstrap")
		}
		if opts.cnLoginBonusPath == "" {
			return errors.New("-cn-login-bonus is required for cn602-bootstrap")
		}
		if opts.cnGachaBannerPath == "" || opts.cnFiveStarGachaBannerPath == "" || opts.cnHomeBannerPath == "" {
			return errors.New("-cn-gacha-banner, -cn-five-star-gacha-banner and -cn-home-banner are required for cn602-bootstrap")
		}
		combatCatalog, catalogErr := multiplayer.LoadCombatCatalog(opts.cnCombatCardPath, opts.cnCombatMasterRoot)
		if catalogErr != nil {
			return fmt.Errorf("load CN combat catalog: %w", catalogErr)
		}
		multiplayerHub = multiplayer.NewHub()
		if attachErr := multiplayerHub.AttachCombatCatalog(combatCatalog); attachErr != nil {
			return fmt.Errorf("attach CN combat catalog: %w", attachErr)
		}
		battleServer, err = multiplayer.NewServer(multiplayerHub, logger)
		if err == nil {
			handler, err = cnbootstrap.New(cnbootstrap.Config{
				Persistence: cnbootstrap.PersistenceConfig{
					RequestLog: opts.requestLog,
					SavePath:   opts.cnSavePath,
					SeedPath:   opts.cnSaveSeedPath,
				},
				Resources: cnbootstrap.ResourcesConfig{
					AssetMap:            opts.cnAssetMapPath,
					GachaBanner:         opts.cnGachaBannerPath,
					FiveStarGachaBanner: opts.cnFiveStarGachaBannerPath,
					HomeBanner:          opts.cnHomeBannerPath,
					CPKRoot:             opts.cnCPKRoot,
					ImageRoot:           opts.cnImageRoot,
					CPKAliases:          opts.cnCPKAliasPath,
					PatchRoots:          []string(opts.cnPatchRoots),
				},
				Masters: cnbootstrap.MastersConfig{
					Cards:             opts.cnCardMasterPath,
					Explore:           opts.cnExploreMasterPath,
					Story:             opts.cnStoryMasterPath,
					Battle:            opts.cnBattleMasterPath,
					Navi:              opts.cnNaviMasterPath,
					Items:             opts.cnItemMasterPath,
					Avatar:            opts.cnAvatarMasterPath,
					Stamps:            opts.cnStampMasterPath,
					Honors:            opts.cnHonorMasterPath,
					PVP:               opts.cnPVPMasterPath,
					PlayerProgression: opts.cnPlayerProgressionPath,
					LoginBonus:        opts.cnLoginBonusPath,
				},
				Network: cnbootstrap.NetworkConfig{
					AdvertiseHost: opts.advertiseHost,
					HTTPPort:      opts.port,
					BattleSV:      multiplayer.Endpoint{Host: opts.advertiseHost, Port: uint16(opts.battlePort)},
				},
				Multiplayer: multiplayerHub,
				CDN:         cdn,
				Logger:      logger,
			})
		}
	default:
		return fmt.Errorf("unsupported client profile %q", opts.clientProfile)
	}
	if err != nil {
		return err
	}

	if closer, ok := handler.(io.Closer); ok {
		defer func() {
			if err := closer.Close(); err != nil {
				logger.Error("close SQLite connection pools", "error", err)
			}
		}()
	}
	server := &http.Server{
		Addr:              opts.listenHost + ":" + strconv.Itoa(opts.port),
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       2 * time.Minute,
	}
	listener, err := net.Listen("tcp", server.Addr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", server.Addr, err)
	}
	defer listener.Close()
	battleAddress := opts.listenHost + ":" + strconv.Itoa(opts.battlePort)
	battleListener, err := net.Listen("tcp", battleAddress)
	if err != nil {
		return fmt.Errorf("listen on BattleSv %s: %w", battleAddress, err)
	}
	defer battleListener.Close()
	var adminServer *http.Server
	var adminListener net.Listener
	if opts.adminPort != 0 {
		provider, ok := handler.(interface{ AdminHandler() http.Handler })
		if !ok || provider.AdminHandler() == nil {
			return errors.New("selected client profile does not provide Admin Web")
		}
		adminServer = &http.Server{
			Addr:              net.JoinHostPort(opts.adminListenHost, strconv.Itoa(opts.adminPort)),
			Handler:           provider.AdminHandler(),
			ReadHeaderTimeout: 5 * time.Second,
			ReadTimeout:       15 * time.Second,
			WriteTimeout:      30 * time.Second,
			IdleTimeout:       2 * time.Minute,
		}
		adminListener, err = net.Listen("tcp", adminServer.Addr)
		if err != nil {
			return fmt.Errorf("listen on Admin Web %s: %w", adminServer.Addr, err)
		}
		defer adminListener.Close()
	}
	attributes := []any{"listen", server.Addr, "advertise_base_url", baseURL}
	attributes = append(attributes, "client_profile", opts.clientProfile, "request_log", opts.requestLog, "battle_listen", battleListener.Addr().String(), "battle_advertise", net.JoinHostPort(opts.advertiseHost, strconv.Itoa(opts.battlePort)))
	if adminListener != nil {
		attributes = append(attributes, "admin_listen", adminListener.Addr().String())
	}
	logger.Info("server ready", attributes...)
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(stop)
	httpErrors := make(chan error, 1)
	go func() { httpErrors <- server.Serve(listener) }()
	battleErrors := make(chan error, 1)
	go func() { battleErrors <- battleServer.Serve(battleListener) }()
	var adminErrors chan error
	if adminServer != nil {
		adminErrors = make(chan error, 1)
		go func() { adminErrors <- adminServer.Serve(adminListener) }()
	}
	shutdown := func() {
		logger.Info("server stopping; draining requests and SQLite transactions")
		// Close all admission paths together. Shared SQLite pools close on
		// return only after every active HTTP/Admin/BattleSv operation drains.
		var draining sync.WaitGroup
		for _, service := range []*http.Server{server, adminServer} {
			if service != nil {
				draining.Add(1)
				go func() { defer draining.Done(); _ = service.Shutdown(context.Background()) }()
			}
		}
		_ = battleListener.Close()
		_ = battleServer.Close()
		draining.Wait()
		logger.Info("server stopped; active operations drained")
	}
	select {
	case <-requested:
		shutdown()
		return nil
	case <-stop:
		shutdown()
		if serveErr := <-httpErrors; serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			return serveErr
		}
		return nil
	case serveErr := <-httpErrors:
		shutdown()
		if errors.Is(serveErr, http.ErrServerClosed) {
			return nil
		}
		return serveErr
	case serveErr := <-battleErrors:
		shutdown()
		if serveErr == nil || errors.Is(serveErr, net.ErrClosed) {
			return nil
		}
		return serveErr
	case serveErr := <-adminErrors:
		shutdown()
		if errors.Is(serveErr, http.ErrServerClosed) {
			return nil
		}
		return serveErr
	}
}
