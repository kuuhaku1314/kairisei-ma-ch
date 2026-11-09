package admin

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"kairisei.local/server/battlecontent"
	"kairisei.local/server/internal/masterdata"
	"kairisei.local/server/internal/protocol"
)

type adminCardResource struct {
	Name   string
	PictID int
	Ready  bool
}

// Catalog visibility and gameplay resource closure are separate from the
// loopback-only thumbnail manifest. Recovered cards may have no web thumbnail.
func loadAdminCardResources(master masterdata.CardRuntimeMaster, masterPath, assetMapPath string) (map[int]adminCardResource, error) {
	file, err := os.Open(assetMapPath)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	var assets struct {
		CatalogAssets []struct {
			Name string `json:"name"`
		} `json:"catalog_assets"`
	}
	if err := json.NewDecoder(io.LimitReader(file, 128*1024*1024)).Decode(&assets); err != nil {
		return nil, err
	}
	names := make(map[string]bool, len(assets.CatalogAssets))
	for _, asset := range assets.CatalogAssets {
		names[strings.ToLower(asset.Name)] = true
	}
	resources := make(map[int]adminCardResource)
	tables := []string{"card.csv"}
	if master.AlchemistStarterDeck != nil {
		tables = append(tables, "card_alchemist.csv")
	}
	for _, name := range tables {
		content, err := os.ReadFile(filepath.Join(filepath.Dir(masterPath), "cn602-card-master", name))
		if err != nil {
			return nil, err
		}
		digest := sha256.Sum256(content)
		verified := false
		for _, source := range master.Source.Files {
			if filepath.Base(filepath.FromSlash(source.Path)) == name && source.Bytes == len(content) && source.SHA256 == hex.EncodeToString(digest[:]) {
				verified = true
			}
		}
		if !verified {
			return nil, fmt.Errorf("admin card source %s differs from the runtime master receipt", name)
		}
		lines := bufio.NewScanner(strings.NewReader(strings.TrimPrefix(string(content), "\ufeff")))
		lines.Buffer(make([]byte, 4096), 4*1024*1024)
		if !lines.Scan() {
			return nil, errors.New("admin card source has no header")
		}
		header := protocol.SplitCSVLine(lines.Text())
		if err := battlecontent.ValidateCardPresentationHeader(header); err != nil {
			return nil, err
		}
		// Use the same physical-line dialect as CardDataContainer / battle master.
		err = battlecontent.ReadCardData(strings.NewReader(string(content)), func(row []string) error {
			card, err := battlecontent.ParseCardPresentation(row)
			if err != nil {
				return err
			}
			if _, duplicate := resources[card.ID]; duplicate {
				return fmt.Errorf("duplicate card identity %d", card.ID)
			}
			pict := card.PictureID
			ready := pict >= 10000000 && names[fmt.Sprintf("chr10_%08d", pict)] && names[fmt.Sprintf("chr20_%08d", pict)]
			resources[card.ID] = adminCardResource{Name: card.Name, PictID: pict, Ready: ready}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	for _, card := range master.CardTemplates {
		if _, ok := resources[card.CardID]; !ok {
			return nil, fmt.Errorf("card %d has no presentation identity", card.CardID)
		}
	}
	return resources, nil
}
