package cnbootstrap

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash/crc32"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"kairisei.local/server/internal/accountstore"

	"github.com/go-chi/chi/v5"

	"kairisei.local/server/internal/testfixture"
)

func writePlatformFixture(t *testing.T, root, platform string, payload []byte) string {
	t.Helper()
	directory := filepath.Join(root, platform)
	if err := os.MkdirAll(filepath.Join(directory, "bundles"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "bundles", "menu.dat"), payload, 0o600); err != nil {
		t.Fatal(err)
	}
	crc := fmt.Sprintf("%08X", crc32.ChecksumIEEE(payload))
	var catalog bytes.Buffer
	zipper := gzip.NewWriter(&catalog)
	if _, err := fmt.Fprintf(zipper, "<v>,790\n<b>,main_c/menu.dat,%s,00000000,%d,N,1,0,0\n", crc, len(payload)); err != nil {
		t.Fatal(err)
	}
	if err := zipper.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "catalog.dat"), catalog.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	hash := func(value []byte) string { sum := sha256.Sum256(value); return hex.EncodeToString(sum[:]) }
	profile := platformDeliveryProfile{
		Platform: platform, UnityVersion: "6000.3.26f1", ContentID: hash([]byte(platform)), ArtifactRoot: ".",
		Catalog: platformDeliveryCatalog{File: "catalog.dat", Bytes: int64(catalog.Len()), SHA256: hash(catalog.Bytes())},
		Bundles: []platformDeliveryBundle{{Name: "main_c/menu.dat", File: "bundles/menu.dat", Bytes: int64(len(payload)), SHA256: hash(payload), CRC32: crc}},
	}
	encoded, err := json.Marshal(profile)
	if err != nil {
		t.Fatal(err)
	}
	profilePath := filepath.Join(directory, "delivery.json")
	if err := os.WriteFile(profilePath, encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	return profilePath
}

func TestPlatformDeliverySelectionAndVersionedBytes(t *testing.T) {
	root := t.TempDir()
	profiles := []string{
		writePlatformFixture(t, root, "StandaloneWindows64", []byte("windows target bytes")),
		writePlatformFixture(t, root, "Android", []byte("android target bytes")),
	}
	deliveries, err := loadPlatformDeliveries(profiles)
	if err != nil {
		t.Fatal(err)
	}
	router := chi.NewRouter()
	registerPlatformContent(router, deliveries, []byte("shared cpk list"), []byte("shared cpk state"))
	for _, platform := range []string{"StandaloneWindows64", "Android"} {
		delivery := deliveries[platformKey(platform, "6000.3.26f1")]
		prefix := "/local/content/" + platform + "/6000.3.26f1/" + delivery.contentID
		var name string
		var file platformDeliveryFile
		for key, object := range delivery.objects {
			if key != "patch/catalog.dat" {
				name, file = key, object.file
			}
		}
		payload, err := os.ReadFile(file.path)
		if err != nil {
			t.Fatal(err)
		}
		request := prefix + "/" + name
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, request, nil))
		if response.Code != http.StatusOK || !bytes.Equal(response.Body.Bytes(), payload) {
			t.Fatalf("%s did not deliver its own target bytes", platform)
		}
		if response.Header().Get("Cache-Control") != "public, max-age=31536000, immutable" || response.Header().Get("ETag") != `"`+file.sha+`"` {
			t.Fatal("versioned content lost its immutable identity")
		}
		lowerCRC := request[:strings.LastIndex(request, ".v")+2] + strings.ToLower(file.crc)
		response = httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, lowerCRC, nil))
		if response.Code != http.StatusOK || !bytes.Equal(response.Body.Bytes(), payload) {
			t.Fatal("case-insensitive CRC lookup changed")
		}
		response = httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodHead, request, nil))
		if response.Code != http.StatusOK || response.Body.Len() != 0 || response.Header().Get("Content-Length") != fmt.Sprint(len(payload)) {
			t.Fatalf("%s HEAD metadata differs", platform)
		}
		response = httptest.NewRecorder()
		rangeRequest := httptest.NewRequest(http.MethodGet, request, nil)
		rangeRequest.Header.Set("Range", "bytes=2-8")
		router.ServeHTTP(response, rangeRequest)
		if response.Code != http.StatusPartialContent || !bytes.Equal(response.Body.Bytes(), payload[2:9]) ||
			response.Header().Get("Content-Range") != fmt.Sprintf("bytes 2-8/%d", len(payload)) || response.Header().Get("ETag") == "" {
			t.Fatalf("%s ranged bytes or metadata differ", platform)
		}
		catalog := delivery.objects["patch/catalog.dat"]
		response = httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, prefix+"/patch/catalog.dat", nil))
		if response.Code != http.StatusOK || !bytes.Equal(response.Body.Bytes(), catalog.data) {
			t.Fatal("catalog no longer serves the pinned startup bytes")
		}
		conditional := httptest.NewRequest(http.MethodGet, prefix+"/patch/catalog.dat", nil)
		conditional.Header.Set("If-None-Match", response.Header().Get("ETag"))
		response = httptest.NewRecorder()
		router.ServeHTTP(response, conditional)
		if response.Code != http.StatusNotModified || response.Body.Len() != 0 {
			t.Fatal("catalog conditional response changed")
		}
		response = httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, strings.Replace(prefix, delivery.contentID, "current", 1)+"/patch/catalog.dat", nil))
		if response.Code != http.StatusOK || response.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("current content alias must not be cached as immutable")
		}
		for _, resource := range []struct{ path, want string }{{"/CPK/cpk_file.csv", "shared cpk list"}, {"/CPK/cpk_patch.txt", "shared cpk state"}} {
			response = httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, prefix+resource.path, nil))
			if response.Code != http.StatusOK || response.Body.String() != resource.want {
				t.Fatal("common CPK metadata was not shared")
			}
		}
		for _, invalid := range []string{
			strings.Replace(request, "/6000.3.26f1/", "/6000.0.46f1/", 1),
			strings.Replace(request, delivery.contentID, strings.Repeat("0", 64), 1),
			prefix + "/patch/main_c/menu.dat.v00000000", prefix + "/patch/undeclared.dat.v12345678",
		} {
			response = httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, invalid, nil))
			if response.Code != http.StatusNotFound {
				t.Fatalf("unexpected delivery for %s: %d", invalid, response.Code)
			}
		}
	}
}

func TestModernLoginSelectsTargetBeforeAccountAndSharesIdentity(t *testing.T) {
	root := t.TempDir()
	set, err := loadPlatformDeliveries([]string{
		writePlatformFixture(t, root, "StandaloneWindows64", []byte("windows")),
		writePlatformFixture(t, root, "Android", []byte("android")),
	})
	if err != nil {
		t.Fatal(err)
	}
	// No store is available: unknown target/engine must fail before minting an account.
	login := platformClientLogin(nil, "127.0.0.1", 26020, CDNConfig{}, "images", set)
	for _, target := range []struct{ platform, unity string }{{"", ""}, {"Android", "6000.0.46f1"}, {"iOS", "6000.3.26f1"}} {
		body, _ := json.Marshal(localLoginPayload{UUID: "d3740000-0000-4000-8000-000000000001", ClientVersion: cnMinimumClientVersion, ResourcePlatform: target.platform, UnityVersion: target.unity})
		response := httptest.NewRecorder()
		login(response, httptest.NewRequest(http.MethodPost, "/local/client/login", bytes.NewReader(body)))
		if response.Code != http.StatusBadRequest {
			t.Fatal("unsupported modern target reached account resolution")
		}
	}
	accounts := testfixture.NewFriendCapacityTestAccounts(t)
	if err := accounts.AttachCreationPolicy(func() accountstore.CreationPolicy {
		return accountstore.CreationPolicy{DailyLimit: 10, VerificationBits: 0}
	}); err != nil {
		t.Fatal(err)
	}
	cdn := CDNConfig{BaseURL: "https://resources.example.invalid/game"}
	login = platformClientLogin(accounts, "127.0.0.1", 26020, cdn, "images", set)
	var userID any
	for _, platform := range []string{"StandaloneWindows64", "Android"} {
		body, _ := json.Marshal(localLoginPayload{UUID: "d3740000-0000-4000-8000-000000000001", ClientVersion: cnMinimumClientVersion, ResourcePlatform: platform, UnityVersion: "6000.3.26f1"})
		response := httptest.NewRecorder()
		login(response, httptest.NewRequest(http.MethodPost, "/local/client/login", bytes.NewReader(body)))
		var result map[string]any
		if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil || response.Code != http.StatusOK || result["res_code"] != float64(0) {
			t.Fatal("modern login failed", response.Body.String(), err)
		}
		delivery := set[platformKey(platform, "6000.3.26f1")]
		identity := "/" + platform + "/6000.3.26f1/" + delivery.contentID + "/"
		if result["version_url"] != "http://127.0.0.1:26020/local/content"+identity || result["res_patch_url"] != cdn.BaseURL+"/content"+identity+"patch/" ||
			result["res_img_url"] != cdn.BaseURL+"/image/images/" || result["res_cpk_url"] != cdn.BaseURL+"/cpk/" {
			t.Fatal("modern login selected inconsistent common/target URLs", result)
		}
		if userID != nil && userID != result["userid"] {
			t.Fatal("platform selection created a second account")
		}
		userID = result["userid"]
	}
}

func TestPlatformDeliveryRejectsCatalogInventoryMismatch(t *testing.T) {
	profilePath := writePlatformFixture(t, t.TempDir(), "Android", []byte("android"))
	encoded, err := os.ReadFile(profilePath)
	if err != nil {
		t.Fatal(err)
	}
	var profile platformDeliveryProfile
	if err := json.Unmarshal(encoded, &profile); err != nil {
		t.Fatal(err)
	}
	profile.Bundles[0].CRC32 = "00000000"
	encoded, err = json.Marshal(profile)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(profilePath, encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadPlatformDeliveries([]string{profilePath}); err == nil {
		t.Fatal("accepted a profile whose catalog declares different delivery bytes")
	}
}
