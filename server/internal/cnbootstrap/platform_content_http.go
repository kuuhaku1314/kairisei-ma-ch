package cnbootstrap

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"os"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"kairisei.local/server/internal/accountstore"
)

func platformClientLogin(accounts *accountstore.Accounts, advertiseHost string, port int, cdn CDNConfig, imageNamespace string, set platformDeliveries) http.HandlerFunc {
	baseURL := "http://" + advertiseHost + ":" + strconv.Itoa(port)
	cpkURL, imageURL := cdn.resourceURLs(baseURL, imageNamespace)
	return localClientLogin(accounts, baseURL, cpkURL, imageURL, func(payload localLoginPayload) (string, string, error) {
		delivery := set.selectDelivery(payload.ResourcePlatform, payload.UnityVersion, "current")
		if delivery == nil {
			return "", "", errors.New("no content for the requested platform and Unity version")
		}
		identity := "/" + delivery.platform + "/" + delivery.unityVersion + "/" + delivery.contentID + "/"
		versionURL := baseURL + "/local/content" + identity
		patchURL := versionURL + "patch/"
		if cdn.BaseURL != "" {
			patchURL = cdn.BaseURL + "/content" + identity + "patch/"
		}
		return patchURL, versionURL, nil
	})
}

func registerPlatformContent(router chi.Router, set platformDeliveries, cpkList, cpkState []byte) {
	router.Route("/local/content/{platform}/{unity}/{content}", func(r chi.Router) {
		r.Use(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				if set.selectDelivery(chi.URLParam(request, "platform"), chi.URLParam(request, "unity"), chi.URLParam(request, "content")) == nil {
					http.NotFound(writer, request)
					return
				}
				next.ServeHTTP(writer, request)
			})
		})
		catalog := func(w http.ResponseWriter, r *http.Request) { set.serveContent(w, r, "patch/catalog.dat") }
		r.Get("/patch/catalog.dat", catalog)
		r.Head("/patch/catalog.dat", catalog)
		r.Get("/patch/*", set.serveBundle)
		r.Head("/patch/*", set.serveBundle)
		movieIndex := func(w http.ResponseWriter, r *http.Request) { set.serveContent(w, r, "movies/index.json") }
		r.Get("/movies/index.json", movieIndex)
		r.Head("/movies/index.json", movieIndex)
		movie := func(w http.ResponseWriter, r *http.Request) {
			set.serveContent(w, r, "movies/"+chi.URLParam(r, "movie"))
		}
		r.Get("/movies/{movie}", movie)
		r.Head("/movies/{movie}", movie)
		r.Get("/CPK/cpk_file.csv", cnBootstrapCPKFileList(cpkList))
		r.Get("/CPK/cpk_patch.txt", cnBootstrapCPKPatchState(cpkState))
	})
}

func (set platformDeliveries) serveContent(w http.ResponseWriter, r *http.Request, name string) {
	delivery := set.selectDelivery(chi.URLParam(r, "platform"), chi.URLParam(r, "unity"), chi.URLParam(r, "content"))
	object, exists := delivery.objects[name]
	if !exists {
		http.NotFound(w, r)
		return
	}
	servePlatformObject(w, r, name, object)
}

func (set platformDeliveries) serveBundle(writer http.ResponseWriter, request *http.Request) {
	delivery := set.selectDelivery(chi.URLParam(request, "platform"), chi.URLParam(request, "unity"), chi.URLParam(request, "content"))
	versioned := chi.URLParam(request, "*")
	object, exists := delivery.objects["patch/"+versioned]
	if !exists {
		suffix := strings.LastIndex(versioned, ".v")
		if suffix <= 0 || len(versioned[suffix+2:]) != 8 {
			http.Error(writer, "invalid platform bundle version", http.StatusBadRequest)
			return
		}
		object, exists = delivery.objects["patch/"+versioned[:suffix]+".v"+strings.ToUpper(versioned[suffix+2:])]
		if !exists {
			http.NotFound(writer, request)
			return
		}
	}
	servePlatformObject(writer, request, versioned, object)
}

func servePlatformObject(writer http.ResponseWriter, request *http.Request, name string, object platformContentObject) {
	if object.data != nil {
		allowResourceTransfer(writer, int64(len(object.data)))
		platformContentHeaders(writer, request, object.file.sha)
		writer.Header().Set("Content-Type", object.contentType)
		http.ServeContent(writer, request, path.Base(name), time.Time{}, bytes.NewReader(object.data))
		return
	}
	servePlatformFile(writer, request, name, object.file, object.contentType)
}

// The source was registered at startup; recheck its current size before serving
// any range, without rehashing or buffering the payload for each request.
func servePlatformFile(writer http.ResponseWriter, request *http.Request, name string, file platformDeliveryFile, contentType string) {
	input, err := os.Open(file.path)
	if err != nil {
		http.Error(writer, "read platform content", http.StatusInternalServerError)
		return
	}
	defer input.Close()
	info, err := input.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() != file.size {
		http.Error(writer, "platform content size changed", http.StatusInternalServerError)
		return
	}
	allowResourceTransfer(writer, info.Size())
	platformContentHeaders(writer, request, file.sha)
	writer.Header().Set("Content-Type", contentType)
	http.ServeContent(writer, request, path.Base(name), info.ModTime(), io.NewSectionReader(input, 0, info.Size()))
}

func platformContentHeaders(writer http.ResponseWriter, request *http.Request, sha string) {
	writer.Header().Set("Content-Type", "application/octet-stream")
	writer.Header().Set("ETag", `"`+sha+`"`)
	writer.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	if chi.URLParam(request, "content") == "current" {
		writer.Header().Set("Cache-Control", "no-store")
	}
}
