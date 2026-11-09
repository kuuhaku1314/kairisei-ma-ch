package cnbootstrap

import (
	"log/slog"

	"kairisei.local/server/battlemodel"
	"kairisei.local/server/internal/multiplayer"
	"kairisei.local/server/internal/netstats"
	"kairisei.local/server/internal/objectstore"
)

// Config groups deployment inputs by owner. Paths refer to the validated runtime
// resource set; creating a server does not rebuild or mutate those inputs.
type Config struct {
	Persistence       PersistenceConfig
	Resources         ResourcesConfig
	Masters           MastersConfig
	Network           NetworkConfig
	NetworkMetrics    *netstats.Counters
	AdminLogFile      string
	Multiplayer       *multiplayer.Hub
	CombatDefinitions *battlemodel.Definitions
	CDN               CDNConfig
	// ImageStorage is the optional cdn-sync.json connection for Admin's operational image sync.
	ImageStorage objectstore.Setting
	Logger       *slog.Logger
}

type PersistenceConfig struct {
	RequestLog string
	SavePath   string
	SeedPath   string
}

type ResourcesConfig struct {
	// AssetMap supplies logical names for Admin card presentation.
	AssetMap           string
	CPKRoot            string
	ImageRoot          string
	CPKAliases         string
	PlatformDeliveries []string
	Banners            map[string]string
}

type MastersConfig struct {
	Cards             string
	Explore           string
	Story             string
	Battle            string
	Navi              string
	Items             string
	Avatar            string
	Stamps            string
	Honors            string
	PVP               string
	PlayerProgression string
	LoginBonus        string
}

type NetworkConfig struct {
	AdvertiseHost string
	HTTPPort      int
	BattleSV      multiplayer.Endpoint
}
