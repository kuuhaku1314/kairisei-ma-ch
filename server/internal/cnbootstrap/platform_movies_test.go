package cnbootstrap

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

func TestPortableMoviesBindContentAndServeOnlyRegisteredBytes(t *testing.T) {
	root := t.TempDir()
	profilePath := writePlatformFixture(t, root, "Android", []byte("bundle"))
	directory := filepath.Dir(profilePath)
	hash := func(b []byte) string { return fmt.Sprintf("%x", sha256.Sum256(b)) }
	write := func(name string, value any) platformDeliveryCatalog {
		t.Helper()
		body, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(filepath.Join(directory, name), body, 0600); err != nil {
			t.Fatal(err)
		}
		return platformDeliveryCatalog{File: name, Bytes: int64(len(body)), SHA256: hash(body)}
	}
	movie := []byte("registered portable mp4 payload")
	if err := os.WriteFile(filepath.Join(directory, "existing.mp4"), movie, 0600); err != nil {
		t.Fatal(err)
	}
	name := "movies/" + hash(movie) + ".mp4"
	index := write("portable-index.json", map[string]any{"format": 1, "target_platform": "Portable", "movies": []any{map[string]any{
		"logical_name": "mov_prologue.cpk", "source_bytes": 123, "source_sha256": hash([]byte("cpk")), "usm_sha256": hash([]byte("usm")),
		"movie_file": name, "movie_bytes": len(movie), "movie_sha256": hash(movie),
	}}})
	media := write("movie-profile.json", platformMovieProfile{Format: 1, TargetPlatform: "Portable", ArtifactRoot: ".", Index: index, Files: []platformMovieArtifact{{Name: name, File: "existing.mp4", Bytes: int64(len(movie)), SHA256: hash(movie)}}})
	raw, err := os.ReadFile(profilePath)
	if err != nil {
		t.Fatal(err)
	}
	var profile platformDeliveryProfile
	if err = json.Unmarshal(raw, &profile); err != nil {
		t.Fatal(err)
	}
	oldID := profile.ContentID
	profile.Movies = &media
	write("delivery.json", profile)
	if _, err = loadPlatformDelivery(profilePath); err == nil {
		t.Fatal("movie index reused an old content ID")
	}
	movies, err := loadPlatformMovies(directory, media)
	if err != nil {
		t.Fatal(err)
	}
	profile.ContentID, err = platformMovieContentID(profile, movies)
	if err != nil {
		t.Fatal(err)
	}
	if profile.ContentID == oldID {
		t.Fatal("movie index did not change identity")
	}
	write("delivery.json", profile)
	set, err := loadPlatformDeliveries([]string{profilePath})
	if err != nil {
		t.Fatal(err)
	}
	objects := make(map[string]cdnObject)
	roots := make(map[string]string)
	contents, err := appendPlatformCDN(objects, roots, []string{profilePath})
	if err != nil || len(contents) != 1 || contents[0].ContentID != profile.ContentID {
		t.Fatalf("movie content is not exported with its platform identity: %v, %v", contents, err)
	}
	cdnPrefix := "content/Android/6000.3.26f1/" + profile.ContentID + "/"
	for resource, expected := range map[string][]byte{"movies/index.json": indexBytes(t, directory, index.File), name: movie} {
		object, exists := objects[cdnPrefix+resource]
		if !exists || object.Bytes != int64(len(expected)) || object.SHA256 != hash(expected) {
			t.Fatalf("registered movie missing from CDN inventory: %s", resource)
		}
		body, err := os.ReadFile(object.SourcePath)
		if err != nil || !bytes.Equal(body, expected) || roots[object.SourceRoot] == "" {
			t.Fatalf("movie CDN source does not match HTTP bytes: %s, %v", resource, err)
		}
	}
	router := chi.NewRouter()
	registerPlatformContent(router, set, nil, nil)
	prefix := "/local/content/Android/6000.3.26f1/" + profile.ContentID + "/"
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		for _, resource := range []string{"movies/index.json", name} {
			w := httptest.NewRecorder()
			router.ServeHTTP(w, httptest.NewRequest(method, prefix+resource, nil))
			if w.Code != 200 || w.Header().Get("ETag") == "" {
				t.Fatal("registered media unavailable", w.Code)
			}
			if method == http.MethodHead && w.Body.Len() != 0 {
				t.Fatal("HEAD emitted movie bytes")
			}
		}
	}
	request := httptest.NewRequest(http.MethodGet, prefix+name, nil)
	request.Header.Set("Range", "bytes=2-8")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, request)
	if w.Code != 206 || !bytes.Equal(w.Body.Bytes(), movie[2:9]) || w.Header().Get("Content-Type") != "video/mp4" {
		t.Fatal("media range differs", w.Code)
	}
	for _, resource := range []string{"movies/other.mp4", "movies/" + strings.Repeat("0", 64) + ".mp4", "movies/movie-profile.json", "existing.mp4"} {
		w = httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, prefix+resource, nil))
		if w.Code != 404 {
			t.Fatal("unregistered media exposed", resource, w.Code)
		}
	}
	profile.Movies = nil
	profile.ContentID = oldID
	write("delivery.json", profile)
	set, err = loadPlatformDeliveries([]string{profilePath})
	if err != nil {
		t.Fatal(err)
	}
	router = chi.NewRouter()
	registerPlatformContent(router, set, nil, nil)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/local/content/Android/6000.3.26f1/"+oldID+"/movies/index.json", nil))
	if w.Code != 404 {
		t.Fatal("profile without movies gained an implicit media fallback")
	}
	objects = make(map[string]cdnObject)
	if _, err := appendPlatformCDN(objects, make(map[string]string), []string{profilePath}); err != nil {
		t.Fatal(err)
	}
	for key := range objects {
		if strings.Contains(key, "/movies/") {
			t.Fatal("profile without movies gained implicit CDN media", key)
		}
	}
}

func indexBytes(t *testing.T, directory, name string) []byte {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(directory, name))
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func TestMovieContentIDMatchesProducerCanonicalJSON(t *testing.T) {
	profile := platformDeliveryProfile{Platform: "Android", UnityVersion: "6000.3.26f1", Catalog: platformDeliveryCatalog{SHA256: strings.Repeat("a", 64)}, Bundles: []platformDeliveryBundle{{Name: "main_c/测试&.dat", Bytes: 4, SHA256: strings.Repeat("b", 64)}}}
	movies := &platformMovieDelivery{identity: map[string]any{"index_bytes": 10, "index_sha256": strings.Repeat("c", 64), "files": []map[string]any{{"name": "movies/" + strings.Repeat("d", 64) + ".mp4", "bytes": 20, "sha256": strings.Repeat("d", 64)}}}}
	actual, err := platformMovieContentID(profile, movies)
	if err != nil || actual != "798f98b32b8b5c408a65deeea8904682a873ef80535710989dfade9e36ee4e8a" {
		t.Fatal("producer/server movie identity differs", actual, err)
	}
}
