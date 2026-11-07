package admin

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"kairisei.local/server/internal/game"
	"kairisei.local/server/internal/gamestate"
	"kairisei.local/server/internal/objectstore"
	"kairisei.local/server/internal/operations"
	"kairisei.local/server/internal/testfixture"
)

// The configured banner reaches storage under the container address path, is read back anonymously,
// and only then may the image source switch; a different stored object is never overwritten.
func TestImageSyncMirrorsConfiguredImagesUnderContainerPath(t *testing.T) {
	var lock sync.Mutex
	stored := map[string][]byte{}
	sha := map[string]string{}
	storage := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lock.Lock()
		defer lock.Unlock()
		if !strings.Contains(r.Header.Get("Authorization"), "Credential=test-access/") {
			t.Error("storage request not signed")
		}
		key, ok := strings.CutPrefix(r.URL.Path, "/bucket/")
		if !ok {
			http.Error(w, "path-style key expected", 400)
			return
		}
		switch r.Method {
		case "HEAD":
			if stored[key] == nil {
				w.WriteHeader(404)
				return
			}
			w.Header().Set("Content-Length", fmt.Sprint(len(stored[key])))
			w.Header().Set("X-Amz-Meta-Sha256", sha[key])
		case "PUT":
			data, _ := io.ReadAll(r.Body)
			if r.Header.Get("If-None-Match") != "*" || r.Header.Get("Content-Type") != "image/png" {
				t.Error("image upload lost its conditional write or type")
			}
			stored[key], sha[key] = data, r.Header.Get("X-Amz-Meta-Sha256")
		default:
			http.Error(w, "unexpected", 405)
		}
	}))
	defer storage.Close()
	public := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lock.Lock()
		data := stored[strings.TrimPrefix(r.URL.Path, "/")]
		lock.Unlock()
		if r.Header.Get("Authorization") != "" || data == nil {
			http.NotFound(w, r)
			return
		}
		if r.Method == "HEAD" {
			w.Header().Set("Content-Length", fmt.Sprint(len(data)))
			return
		}
		w.Header().Set("Content-Range", fmt.Sprintf("bytes 0-63/%d", len(data)))
		w.WriteHeader(206)
		w.Write(data[:64])
	}))
	defer public.Close()

	accounts := testfixture.NewFriendCapacityTestAccounts(t)
	o, err := NewOperations(accounts.Database(), nil)
	if err != nil {
		t.Fatal(err)
	}
	days := []gamestate.LoginBonusDay{{Day: 1, Comment: "签到", Reward: gamestate.Reward{Type: 10, Num: 1, CardSkillLevels: []int16{}}}}
	o.playerDefaults = &PlayerPolicy{Notice: noticePolicy{Title: "公告", Body: "欢迎"}, Login: loginRewards{days, days, days}, StoryCrystals: 50, Navigators: []game.NaviSetting{}}
	o.playerLoginBase = gamestate.LoginBonusPolicy{Cycle: days, Beginner: days, TotalMilestones: days}
	if err := o.loadPlayerPolicy(); err != nil {
		t.Fatal(err)
	}
	credentials := &objectstore.Credentials{EndpointURL: storage.URL, Bucket: "bucket", AccessKeyID: "test-access", SecretAccessKey: "test-secret", Region: "auto", AddressingStyle: "path"}
	a := &API{operations: o, gachaCoverDir: t.TempDir(), homeBannerDir: t.TempDir(), catalogByKey: map[string]AdminCatalogEntry{},
		imageStorage: objectstore.Setting{Credentials: credentials}, imageSync: &imageSyncState{}}
	router := chi.NewRouter()
	router.Post("/upload", a.uploadHomeBanner)
	router.Put("/policy", a.savePlayerPolicy)
	router.Put("/settings", a.setRuntimeSettings)
	router.Get("/sync", a.imageSyncStatus)
	router.Post("/sync", a.startImageSync)
	router.Post("/activate", a.activateImageSync)
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, image.NewRGBA(image.Rect(0, 0, 128, 64))); err != nil {
		t.Fatal(err)
	}
	var upload struct {
		CoverPath string `json:"cover_path"`
	}
	json.Unmarshal(testfixture.CallContentAdmin(t, router, "POST", "/upload", map[string]any{"data_base64": base64.StdEncoding.EncodeToString(encoded.Bytes())}, 200), &upload)
	config := *o.playerDefaults
	config.HomeBanners = operations.HomeBannerPolicy{Custom: true, Items: []operations.HomeBannerEntry{{Title: "活动", Enabled: true, CoverPath: upload.CoverPath, Target: "none"}}}
	testfixture.CallContentAdmin(t, router, "PUT", "/policy", map[string]any{"expected_revision": 0, "config": config}, 200)

	type status struct {
		Images   []imageSyncImage `json:"images"`
		Digest   string           `json:"digest"`
		Problem  string           `json:"base_problem"`
		Prefix   string           `json:"object_prefix"`
		Revision int              `json:"settings_revision"`
		Source   string           `json:"source"`
	}
	read := func() (s status) {
		t.Helper()
		if err := json.Unmarshal(testfixture.CallContentAdmin(t, router, "GET", "/sync", nil, 200), &s); err != nil {
			t.Fatal(err)
		}
		return s
	}
	run := func(mode string, s status) *imageSyncJob {
		t.Helper()
		testfixture.CallContentAdmin(t, router, "POST", "/sync", map[string]any{"mode": mode, "digest": s.Digest}, 202)
		for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
			if job := a.imageSync.snapshot(); job.State == "complete" {
				return job
			}
		}
		t.Fatal("image sync did not finish")
		return nil
	}
	if s := read(); s.Problem == "" || len(s.Images) != 1 || !s.Images[0].Local || s.Images[0].Uses[0].Note != "展示中" {
		t.Fatalf("unconfigured address or banner inventory: %+v", s)
	}
	// The container address ends in a directory; that path is the key prefix.
	base := strings.Replace(public.URL, "127.0.0.1", "localhost", 1) + "/ops/"
	testfixture.CallContentAdmin(t, router, "PUT", "/settings", map[string]any{"crystal_purchase_enabled": false, "expected_revision": o.runtimeSettingsRevision,
		"gacha_cover_source": "server", "gacha_cover_base_url": base}, 200)
	s := read()
	if s.Problem != "" || s.Prefix != "ops/" {
		t.Fatalf("container path is not the key prefix: %+v", s)
	}
	testfixture.CallContentAdmin(t, router, "POST", "/sync", map[string]any{"mode": "sync", "digest": "stale"}, 409)
	if job := run("check", s); job.Results[upload.CoverPath].Remote != "missing" || job.ready(s.Digest, base) {
		t.Fatalf("check: %+v", job)
	}
	testfixture.CallContentAdmin(t, router, "POST", "/activate", map[string]any{"digest": s.Digest, "expected_revision": s.Revision}, 400)
	job := run("sync", s)
	if result := job.Results[upload.CoverPath]; result.Remote != "uploaded" || result.Public != "ok" || !job.ready(s.Digest, base) {
		t.Fatalf("sync: %+v", job)
	}
	if !bytes.Equal(stored["ops/"+upload.CoverPath], encoded.Bytes()) {
		t.Fatal("object key is not container path plus the existing reference")
	}
	audit, err := o.storage.ReadDocument(imageSyncKey)
	if err != nil || audit.Revision != 1 || bytes.Contains(audit.Payload, []byte("test-secret")) || bytes.Contains(audit.Payload, []byte("test-access")) {
		t.Fatal("sync record missing or carries credentials", err)
	}
	testfixture.CallContentAdmin(t, router, "POST", "/activate", map[string]any{"digest": s.Digest, "expected_revision": s.Revision}, 200)
	if s := read(); s.Source != "storage" || o.runtimeSettings.GachaCoverBaseURL != base {
		t.Fatal("verified sync did not switch the image source")
	}
	lock.Lock()
	sha["ops/"+upload.CoverPath] = "different"
	lock.Unlock()
	if job := run("sync", read()); job.Results[upload.CoverPath].Remote != "conflict" {
		t.Fatalf("a different stored object was not reported: %+v", job)
	}
}
