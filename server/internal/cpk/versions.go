package cpk

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// VersionsFileName belongs to the resource set and is registered in its manifest.
const VersionsFileName = "versions.json"
const maxVersionManifestBytes = 1 << 20
const maxClientVersion = 1<<31 - 1 // CN parses the URL suffix as a signed Int32.

type versionPolicy map[string]uint64

func (versions versionPolicy) version(name string) uint64 {
	if value, ok := versions[strings.ToLower(name)]; ok {
		return value
	}
	return 1
}

func loadVersions(root string) (versionPolicy, error) {
	file, err := os.Open(filepath.Join(root, VersionsFileName))
	if err != nil {
		return nil, err
	}
	defer file.Close()
	content, err := io.ReadAll(io.LimitReader(file, maxVersionManifestBytes+1))
	if err != nil {
		return nil, err
	}
	return decodeVersions(content)
}

func decodeVersions(content []byte) (versionPolicy, error) {
	if len(content) == 0 || len(content) > maxVersionManifestBytes {
		return nil, errors.New("version metadata must be non-empty and at most one MiB")
	}
	var manifest struct {
		Versions []struct {
			Name    string `json:"name"`
			Version uint64 `json:"version"`
		} `json:"versions"`
	}
	decoder := json.NewDecoder(bytes.NewReader(content))
	if err := decoder.Decode(&manifest); err != nil {
		return nil, err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return nil, errors.New("trailing data in version metadata")
	}
	result := make(versionPolicy, len(manifest.Versions))
	for _, entry := range manifest.Versions {
		name := strings.ToLower(entry.Name)
		if !ValidName(entry.Name) || entry.Version == 0 || entry.Version > maxClientVersion {
			return nil, fmt.Errorf("invalid CPK version entry %q=%d", entry.Name, entry.Version)
		}
		if _, exists := result[name]; exists {
			return nil, fmt.Errorf("duplicate CPK version %q", entry.Name)
		}
		result[name] = entry.Version
	}
	return result, nil
}
