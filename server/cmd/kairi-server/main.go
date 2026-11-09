package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"kairisei.local/server/battlemodel"
	"kairisei.local/server/internal/cdnsync"
	"kairisei.local/server/internal/cnbootstrap"
	"kairisei.local/server/internal/multiplayer"
	"kairisei.local/server/internal/netstats"
)

const serverVersion = "2.0.2"

type options struct {
	showVersion       bool
	cdnSyncConfig     string
	imageStorage      string
	cdnSyncDryRun     bool
	cdnConfig         string
	cdnManifestOutput string
	resourceSet       string

	requestLog            string
	adminLogFile          string
	cnSavePath            string
	platformContent       stringListFlag
	platformContentConfig string
	listenHost            string
	advertiseHost         string
	port                  int
	battlePort            int
	adminListenHost       string
	adminPort             int
	shutdownEvent         string
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

// The assembled deployment already owns the selected profile list. Resolve only
// that field here; the normal platform loader still validates every profile.
func loadPlatformContentConfig(filename string) (stringListFlag, error) {
	absolute, err := filepath.Abs(filename)
	if err != nil {
		return nil, err
	}
	root, err := filepath.EvalSymlinks(filepath.Dir(absolute))
	if err != nil {
		return nil, err
	}
	content, err := os.ReadFile(absolute)
	if err != nil {
		return nil, err
	}
	var config struct {
		PlatformContents []string `json:"platform_contents"`
	}
	if err := json.Unmarshal(content, &config); err != nil {
		return nil, err
	}
	if len(config.PlatformContents) == 0 {
		return nil, errors.New("deployment platform_contents must contain at least one profile")
	}
	profiles := make(stringListFlag, 0, len(config.PlatformContents))
	for _, name := range config.PlatformContents {
		if name == "." || !filepath.IsLocal(name) || path.Clean(name) != name || strings.ContainsAny(name, "\\:\x00\r\n") {
			return nil, fmt.Errorf("invalid deployment platform profile path %q", name)
		}
		resolved, err := filepath.EvalSymlinks(filepath.Join(root, filepath.FromSlash(name)))
		if err != nil {
			return nil, err
		}
		relative, err := filepath.Rel(root, resolved)
		if err != nil || !filepath.IsLocal(relative) {
			return nil, fmt.Errorf("deployment platform profile escapes package root: %q", name)
		}
		profiles = append(profiles, resolved)
	}
	return profiles, nil
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
	flags.StringVar(&opts.imageStorage, "cdn-sync-config", "", "optional cdn-sync.json whose storage keys let Admin sync operational images; missing or unfilled keeps it off")
	flags.StringVar(&opts.resourceSet, "resource-set", "resource-set/resource-set.json", "complete resource-set.json for server resources and CDN export/sync")
	flags.StringVar(&opts.shutdownEvent, "shutdown-event", "", "Windows local graceful shutdown event")

	flags.StringVar(&opts.requestLog, "request-log", "", "CN JSONL request capture")
	flags.StringVar(&opts.adminLogFile, "admin-log-file", "", "current server JSON log file for read-only Admin search")
	flags.StringVar(&opts.cnSavePath, "cn-save-path", "", "local account database path prefix")
	flags.Var(&opts.platformContent, "platform-content", "target/Unity delivery.json; repeat per platform")
	flags.StringVar(&opts.platformContentConfig, "platform-content-config", "", "assembled deployment.json; read platform_contents relative to its directory instead of -platform-content")
	flags.StringVar(&opts.listenHost, "listen-host", "0.0.0.0", "local listen address")
	flags.StringVar(&opts.advertiseHost, "advertise-host", "10.0.2.2", "host returned to client")
	flags.IntVar(&opts.port, "port", 26020, "local HTTP port")
	flags.IntVar(&opts.battlePort, "battle-port", 0, "local BattleSv TCP port; defaults to HTTP port plus one")
	flags.StringVar(&opts.adminListenHost, "admin-listen-host", "127.0.0.1", "loopback-only Admin Web listen address")
	flags.IntVar(&opts.adminPort, "admin-port", 0, "loopback-only Admin Web port; zero disables it")
	if err := flags.Parse(arguments); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("unexpected positional arguments")
	}
	if opts.showVersion {
		_, err := fmt.Fprintln(os.Stdout, "kairi-server "+serverVersion)
		return err
	}
	if opts.platformContentConfig != "" {
		if len(opts.platformContent) != 0 {
			return errors.New("-platform-content-config and -platform-content cannot be combined")
		}
		profiles, err := loadPlatformContentConfig(opts.platformContentConfig)
		if err != nil {
			return fmt.Errorf("load platform content config: %w", err)
		}
		opts.platformContent = profiles
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
		return cdnsync.Run(ctx, cdnsync.Options{ConfigPath: opts.cdnSyncConfig, ResourceSet: opts.resourceSet, PlatformContent: []string(opts.platformContent), CDNConfigPath: opts.cdnConfig, DryRun: opts.cdnSyncDryRun}, os.Stdout)
	}
	if opts.cdnSyncDryRun {
		return errors.New("-cdn-sync-dry-run requires -sync-cdn")
	}
	if opts.cdnManifestOutput != "" {
		return cnbootstrap.ExportCDNManifest(opts.resourceSet, opts.cdnManifestOutput, opts.platformContent...)
	}
	cdn, cdnErr := cnbootstrap.LoadCDNConfig(opts.cdnConfig)
	// The game service never depends on upload keys: a faulty file only disables the Admin image sync.
	imageStorage := cdnsync.LoadImageStorage(opts.imageStorage)
	if imageStorage.Problem != "" {
		logger.Warn("Admin image sync disabled", "config", opts.imageStorage, "problem", imageStorage.Problem)
	}
	if cdnErr != nil {
		return cdnErr
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
	networkMetrics := &netstats.Counters{}
	inputs, err := cnbootstrap.LoadRuntimeInputs(opts.resourceSet)
	if err != nil {
		return err
	}
	inputs.Resources.PlatformDeliveries = []string(opts.platformContent)
	combatCatalog, catalogErr := multiplayer.LoadCombatCatalog(inputs.CombatCardMaster, inputs.CombatMasterRoot)
	if catalogErr != nil {
		return fmt.Errorf("load CN combat catalog: %w", catalogErr)
	}
	multiplayerHub := multiplayer.NewHub()
	if attachErr := multiplayerHub.AttachCombatCatalog(combatCatalog); attachErr != nil {
		return fmt.Errorf("attach CN combat catalog: %w", attachErr)
	}
	battleServer, err := multiplayer.NewServer(multiplayerHub, logger)
	if err != nil {
		return err
	}
	handler, err := cnbootstrap.New(cnbootstrap.Config{
		NetworkMetrics:    networkMetrics,
		AdminLogFile:      opts.adminLogFile,
		CombatDefinitions: (*battlemodel.Definitions)(combatCatalog),
		Persistence: cnbootstrap.PersistenceConfig{
			RequestLog: opts.requestLog,
			SavePath:   opts.cnSavePath,
			SeedPath:   inputs.SeedPath,
		},
		Resources: inputs.Resources,
		Masters:   inputs.Masters,
		Network: cnbootstrap.NetworkConfig{
			AdvertiseHost: opts.advertiseHost,
			HTTPPort:      opts.port,
			BattleSV:      multiplayer.Endpoint{Host: opts.advertiseHost, Port: uint16(opts.battlePort)},
		},
		Multiplayer:  multiplayerHub,
		CDN:          cdn,
		ImageStorage: imageStorage,
		Logger:       logger,
	})
	if err != nil {
		return err
	}
	defer func() {
		if err := handler.Close(); err != nil {
			logger.Error("close SQLite connection pools", "error", err)
		}
	}()
	// The synchronized Unity client uses HTTP/2 prior knowledge on plain HTTP.
	// Keep HTTP/1 for resources, browsers and operational clients on this port.
	protocols := new(http.Protocols)
	protocols.SetHTTP1(true)
	protocols.SetUnencryptedHTTP2(true)
	server := &http.Server{
		Protocols:         protocols,
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
	listener = networkMetrics.HTTP.WrapListener(listener)
	defer listener.Close()
	battleAddress := opts.listenHost + ":" + strconv.Itoa(opts.battlePort)
	battleListener, err := net.Listen("tcp", battleAddress)
	if err != nil {
		return fmt.Errorf("listen on BattleSv %s: %w", battleAddress, err)
	}
	battleListener = networkMetrics.BattleTCP.WrapListener(battleListener)
	battleListener = peerFilteredListener{Listener: battleListener, blocked: handler.IPBlocked}
	defer battleListener.Close()
	var adminServer *http.Server
	var adminListener net.Listener
	if opts.adminPort != 0 {
		adminServer = &http.Server{
			Addr:              net.JoinHostPort(opts.adminListenHost, strconv.Itoa(opts.adminPort)),
			Handler:           handler.AdminHandler(),
			ReadHeaderTimeout: 5 * time.Second,
			ReadTimeout:       15 * time.Second,
			WriteTimeout:      30 * time.Second,
			IdleTimeout:       2 * time.Minute,
		}
		adminListener, err = net.Listen("tcp", adminServer.Addr)
		if err != nil {
			return fmt.Errorf("listen on Admin Web %s: %w", adminServer.Addr, err)
		}
		adminListener = networkMetrics.Admin.WrapListener(adminListener)
		defer adminListener.Close()
	}
	if err := handler.Activate(); err != nil {
		return fmt.Errorf("activate account service: %w", err)
	}
	attributes := []any{"listen", server.Addr, "advertise_base_url", baseURL}
	attributes = append(attributes, "client_profile", "cn602-bootstrap", "request_log", opts.requestLog, "battle_listen", battleListener.Addr().String(), "battle_advertise", net.JoinHostPort(opts.advertiseHost, strconv.Itoa(opts.battlePort)))
	if adminListener != nil {
		attributes = append(attributes, "admin_listen", adminListener.Addr().String(), "admin_image_sync", imageStorage.Credentials != nil)
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
		drainContext, cancelDrain := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancelDrain()
		var draining sync.WaitGroup
		for _, service := range []*http.Server{server, adminServer} {
			if service != nil {
				draining.Add(1)
				go func() {
					defer draining.Done()
					if err := service.Shutdown(drainContext); err != nil {
						logger.Warn("HTTP shutdown grace period ended; closing connections", "address", service.Addr, "error", err)
						_ = service.Close()
					}
				}()
			}
		}
		_ = battleListener.Close()
		_ = battleServer.Close()
		draining.Wait()
		logger.Info("server stopped; HTTP shutdown completed")
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
