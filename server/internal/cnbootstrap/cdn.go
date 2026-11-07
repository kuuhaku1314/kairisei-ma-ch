package cnbootstrap

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"strings"
)

// CDNConfig contains public download settings only. Credentials belong to the
// separate sync configuration, never to game login responses.
type CDNConfig struct {
	BaseURL string `json:"base_url"`
}

func LoadCDNConfig(filename string) (CDNConfig, error) {
	var config CDNConfig
	if filename == "" {
		return config, nil
	}
	content, err := os.ReadFile(filename)
	if err != nil {
		return config, fmt.Errorf("read CDN config: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(bytes.TrimPrefix(content, []byte{0xef, 0xbb, 0xbf})))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&config); err != nil {
		return config, fmt.Errorf("decode CDN config: %w", err)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return config, errors.New("CDN config must contain one JSON object")
	}
	return config.normalized()
}

func (config CDNConfig) normalized() (CDNConfig, error) {
	base, err := NormalizeCDNBaseURL(config.BaseURL)
	return CDNConfig{BaseURL: base}, err
}

func NormalizeCDNBaseURL(value string) (string, error) {
	if value == "" {
		return "", nil
	}
	u, err := url.Parse(value)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Hostname() == "" ||
		u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" ||
		strings.ContainsAny(value, "\\\r\n\t ?#") {
		return "", errors.New("CDN base_url must be an absolute HTTP(S) URL without credentials, query or fragment")
	}
	return strings.TrimRight(u.String(), "/"), nil
}

func (config CDNConfig) resourceURLs(serverURL, imageNamespace string) (cpkURL, imageURL string) {
	base := config.BaseURL
	if base == "" {
		base = serverURL + "/local/resources"
	}
	return base + "/cpk/", base + "/image/" + imageNamespace + "/"
}
