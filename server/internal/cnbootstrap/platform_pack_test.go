package cnbootstrap

import (
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"hash/crc32"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPlatformDeliveryRequiresOneTransportPerRealFile(t *testing.T) {
	write := func(t *testing.T, filename string, data []byte) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(filename), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filename, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	type change func(*testing.T, *platformDeliveryProfile, string, []byte)
	cases := []struct {
		name   string
		change change
	}{
		{"validWithoutLoosePackedFiles", nil},
		{"packedAndStandalone", func(t *testing.T, p *platformDeliveryProfile, _ string, _ []byte) {
			p.Bundles[0].File = p.Bundles[0].Name
			write(t, filepath.Join(p.ArtifactRoot, p.Bundles[0].File), []byte("first"))
		}},
		{"memberInTwoPacks", func(t *testing.T, p *platformDeliveryProfile, directory string, archive []byte) {
			other := p.Packs[0]
			other.Name = strings.Replace(other.Name, "tests-", "other-", 1)
			p.Packs = append(p.Packs, other)
			write(t, filepath.Join(directory, other.Name), archive)
		}},
		{"memberTwiceInOnePack", func(_ *testing.T, p *platformDeliveryProfile, _ string, _ []byte) {
			p.Packs[0].Members = append(p.Packs[0].Members, p.Packs[0].Members[0])
		}},
		{"missingTransport", func(_ *testing.T, p *platformDeliveryProfile, _ string, _ []byte) {
			p.Packs[0].Members = p.Packs[0].Members[1:]
		}},
		{"unknownMember", func(_ *testing.T, p *platformDeliveryProfile, _ string, _ []byte) {
			p.Packs[0].Members[0] = "main_c/unknown.dat"
		}},
		{"unlistedStandaloneFileStillInsideArchive", func(t *testing.T, p *platformDeliveryProfile, _ string, _ []byte) {
			p.Bundles[0].File = p.Bundles[0].Name
			write(t, filepath.Join(p.ArtifactRoot, p.Bundles[0].File), []byte("first"))
			p.Packs[0].Members = p.Packs[0].Members[1:]
		}},
		{"archiveCRCNotManifest", func(_ *testing.T, p *platformDeliveryProfile, _ string, _ []byte) {
			p.Bundles[1].CRC32 = "00000001"
		}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			artifacts, directory := t.TempDir(), t.TempDir()
			var archive bytes.Buffer
			writer := zip.NewWriter(&archive)
			p := platformDeliveryProfile{Platform: "StandaloneWindows64", UnityVersion: "6000.3.26f1",
				ContentID: strings.Repeat("a", 64), ArtifactRoot: artifacts}
			members := []string{}
			for i, text := range []string{"first", "second", "third", "independent"} {
				data := []byte(text)
				row := platformDeliveryBundle{Name: fmt.Sprintf("main_c/%c.dat", 'a'+i), Bytes: int64(len(data)),
					SHA256: fmt.Sprintf("%x", sha256.Sum256(data)), CRC32: fmt.Sprintf("%08X", crc32.ChecksumIEEE(data))}
				if i == 3 {
					row.File = row.Name
					write(t, filepath.Join(artifacts, row.File), data)
				} else {
					entry, err := writer.CreateHeader(&zip.FileHeader{Name: row.Name, Method: zip.Store})
					if err != nil {
						t.Fatal(err)
					}
					if _, err := entry.Write(data); err != nil {
						t.Fatal(err)
					}
					members = append(members, row.Name)
				}
				p.Bundles = append(p.Bundles, row)
			}
			if err := writer.Close(); err != nil {
				t.Fatal(err)
			}
			sha := fmt.Sprintf("%x", sha256.Sum256(archive.Bytes()))
			pack := platformDeliveryPack{Name: "packs/tests-" + sha + ".zip", Bytes: int64(archive.Len()), SHA256: sha, Members: members}
			p.Packs = []platformDeliveryPack{pack}
			write(t, filepath.Join(directory, pack.Name), archive.Bytes())
			if test.change != nil {
				test.change(t, &p, directory, archive.Bytes())
			}
			var catalog bytes.Buffer
			compressed := gzip.NewWriter(&catalog)
			for _, row := range p.Bundles {
				fmt.Fprintf(compressed, "<b>,%s,%s,00000000,%d,N,1,0,0\n", row.Name, row.CRC32, row.Bytes)
			}
			for _, pack := range p.Packs {
				fmt.Fprintf(compressed, "<p>,%s,%s,%d,%s\n", pack.Name, pack.SHA256, pack.Bytes, strings.Join(pack.Members, ","))
			}
			if err := compressed.Close(); err != nil {
				t.Fatal(err)
			}
			p.Catalog = platformDeliveryCatalog{File: "catalog.dat", Bytes: int64(catalog.Len()), SHA256: fmt.Sprintf("%x", sha256.Sum256(catalog.Bytes()))}
			write(t, filepath.Join(directory, p.Catalog.File), catalog.Bytes())
			content, err := json.Marshal(p)
			if err != nil {
				t.Fatal(err)
			}
			filename := filepath.Join(directory, "delivery.json")
			write(t, filename, content)
			loaded, err := loadPlatformDelivery(filename)
			if test.change == nil {
				if err != nil || len(loaded.objects) != 3 {
					t.Fatalf("valid transport partition rejected: %v", err)
				}
				if available := (platformDeliveries{"fixture": loaded}).availableBundles(); len(available) != len(p.Bundles) {
					t.Fatal("business inventory lost owners carried only inside packs")
				}
				objects := make(map[string]cdnObject)
				if _, err := appendPlatformCDN(objects, make(map[string]string), []string{filename}); err != nil || len(objects) != 3 {
					t.Fatalf("CDN inventory lost the standalone/pack transport partition: %v", err)
				}
			} else if err == nil {
				t.Fatal("ambiguous or incomplete transport partition accepted")
			}
		})
	}
}
