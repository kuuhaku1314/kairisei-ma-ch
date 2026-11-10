package cpk

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

type AliasManifest struct {
	Aliases []AliasEntry `json:"aliases"`
}

type AliasEntry struct {
	AliasCPKName  string `json:"alias_cpk_name"`
	SourceCPKName string `json:"source_cpk_name"`
}

type Alias struct {
	AliasName  string
	SourceName string
	SourcePath string
}

func ValidName(name string) bool {
	return name != "" &&
		name == filepath.Base(name) && name == path.Base(name) &&
		strings.EqualFold(filepath.Ext(name), ".cpk") &&
		!strings.ContainsAny(name, "/\\,\r\n")
}

func LoadAliases(cpkRoot string, manifestPath string) ([]Alias, error) {
	if manifestPath == "" {
		return nil, errors.New("generated CN CPK alias manifest is required")
	}
	absoluteRoot, err := filepath.Abs(cpkRoot)
	if err != nil {
		return nil, fmt.Errorf("resolve official CN CPK root: %w", err)
	}
	absoluteManifest, err := filepath.Abs(manifestPath)
	if err != nil {
		return nil, fmt.Errorf("resolve CN CPK alias manifest: %w", err)
	}
	content, err := os.ReadFile(absoluteManifest)
	if err != nil {
		return nil, fmt.Errorf("read CN CPK alias manifest: %w", err)
	}
	if len(content) == 0 || len(content) > 1024*1024 {
		return nil, errors.New("CN CPK alias manifest must be non-empty and at most one MiB")
	}
	var manifest AliasManifest
	decoder := json.NewDecoder(bytes.NewReader(content))
	if err := decoder.Decode(&manifest); err != nil {
		return nil, fmt.Errorf("decode CN CPK alias manifest: %w", err)
	}
	seen := make(map[string]struct{}, len(manifest.Aliases))
	resolved := make([]Alias, 0, len(manifest.Aliases))
	for _, alias := range manifest.Aliases {
		if !ValidName(alias.AliasCPKName) || !ValidName(alias.SourceCPKName) {
			return nil, fmt.Errorf("unsafe CN CPK alias %q -> %q", alias.AliasCPKName, alias.SourceCPKName)
		}
		if strings.EqualFold(alias.AliasCPKName, alias.SourceCPKName) {
			return nil, fmt.Errorf("CN CPK alias %q points to itself", alias.AliasCPKName)
		}
		key := strings.ToLower(alias.AliasCPKName)
		if _, exists := seen[key]; exists {
			return nil, fmt.Errorf("duplicate CN CPK alias %q", alias.AliasCPKName)
		}
		seen[key] = struct{}{}
		aliasPath := filepath.Join(absoluteRoot, alias.AliasCPKName)
		if _, err := os.Stat(aliasPath); err == nil {
			return nil, fmt.Errorf("CN CPK alias %q collides with an official file", alias.AliasCPKName)
		} else if !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("stat CN CPK alias target %q: %w", alias.AliasCPKName, err)
		}
		sourcePath := filepath.Join(absoluteRoot, alias.SourceCPKName)
		info, err := os.Stat(sourcePath)
		if err != nil {
			return nil, fmt.Errorf("stat CN CPK alias source %q: %w", alias.SourceCPKName, err)
		}
		if !info.Mode().IsRegular() || info.Size() <= 0 {
			return nil, fmt.Errorf("CN CPK alias source %q is not a non-empty regular file", alias.SourceCPKName)
		}
		resolved = append(resolved, Alias{
			AliasName: alias.AliasCPKName, SourceName: alias.SourceCPKName, SourcePath: sourcePath,
		})
	}
	sort.Slice(resolved, func(i, j int) bool {
		return strings.ToLower(resolved[i].AliasName) < strings.ToLower(resolved[j].AliasName)
	})
	return resolved, nil
}
