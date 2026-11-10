package cnbootstrap

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// RuntimeInputs resolves the resource set's existing entrypoints once at startup.
// Mutable deployment settings and account paths are supplied separately.
type RuntimeInputs struct {
	SeedPath         string
	Resources        ResourcesConfig
	Masters          MastersConfig
	CombatCardMaster string
	CombatMasterRoot string
}

func LoadRuntimeInputs(filename string) (RuntimeInputs, error) {
	var inputs RuntimeInputs
	absolute, err := filepath.Abs(filename)
	if err != nil {
		return inputs, err
	}
	absolute, err = filepath.EvalSymlinks(absolute)
	if err != nil {
		return inputs, fmt.Errorf("resolve resource-set manifest: %w", err)
	}
	info, err := os.Stat(absolute)
	if err != nil {
		return inputs, err
	}
	if !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > 32<<20 {
		return inputs, errors.New("resource-set manifest must be a non-empty JSON file within 32 MiB")
	}
	content, err := os.ReadFile(absolute)
	if err != nil {
		return inputs, err
	}
	var manifest struct {
		Entrypoints map[string]string `json:"entrypoints"`
	}
	if err := json.Unmarshal(content, &manifest); err != nil {
		return inputs, fmt.Errorf("decode resource-set manifest: %w", err)
	}
	bindings := []struct {
		key string
		out *string
	}{
		{"cn-save-seed", &inputs.SeedPath},
		{"cn-asset-map", &inputs.Resources.AssetMap},
		{"cn-cpk-root", &inputs.Resources.CPKRoot},
		{"cn-image-root", &inputs.Resources.ImageRoot},
		{"cn-cpk-aliases", &inputs.Resources.CPKAliases},
		{"cn-card-master", &inputs.Masters.Cards},
		{"cn-explore-master", &inputs.Masters.Explore},
		{"cn-story-master", &inputs.Masters.Story},
		{"cn-battle-master", &inputs.Masters.Battle},
		{"cn-navi-master", &inputs.Masters.Navi},
		{"cn-item-master", &inputs.Masters.Items},
		{"cn-avatar-master", &inputs.Masters.Avatar},
		{"cn-stamp-master", &inputs.Masters.Stamps},
		{"cn-honor-master", &inputs.Masters.Honors},
		{"cn-pvp-master", &inputs.Masters.PVP},
		{"cn-player-progression", &inputs.Masters.PlayerProgression},
		{"cn-login-bonus", &inputs.Masters.LoginBonus},
		{"cn-combat-card-master", &inputs.CombatCardMaster},
		{"cn-combat-master-root", &inputs.CombatMasterRoot},
	}
	for _, binding := range bindings {
		*binding.out, err = cdnSourcePath(filepath.Dir(absolute), manifest.Entrypoints[binding.key])
		if err != nil {
			return RuntimeInputs{}, fmt.Errorf("resource-set entrypoint %s: %w", binding.key, err)
		}
	}
	inputs.Resources.Banners = make(map[string]string, 3)
	for key, entrypoint := range map[string]string{"": "cn-gacha-banner", "five_star_ticket": "cn-five-star-gacha-banner", "home": "cn-home-banner"} {
		filename, err := cdnSourcePath(filepath.Dir(absolute), manifest.Entrypoints[entrypoint])
		if err != nil {
			return RuntimeInputs{}, fmt.Errorf("resource-set entrypoint %s: %w", entrypoint, err)
		}
		inputs.Resources.Banners[key] = filename
	}
	return inputs, nil
}
