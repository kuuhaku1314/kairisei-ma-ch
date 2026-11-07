package cnbootstrap

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"kairisei.local/server/internal/cpk"
)

func cnBootstrapCPKResource(delivery []cpk.File) http.HandlerFunc {
	files := make(map[string]cpk.File, len(delivery))
	for _, file := range delivery {
		files[strings.ToLower(file.Name)] = file
	}
	return func(writer http.ResponseWriter, request *http.Request) {
		// The CN client appends its own CPK directory to res_cpk_url.
		name := strings.TrimPrefix(chi.URLParam(request, "*"), "CPK/")
		versionText := ""
		if suffixAt := strings.LastIndex(name, ".v"); suffixAt > 0 {
			versionText, name = name[suffixAt+2:], name[:suffixAt]
			if versionText == "" {
				http.Error(writer, "invalid CPK version", http.StatusBadRequest)
				return
			}
		}
		if !cpk.ValidName(name) {
			http.Error(writer, "invalid CPK file name", http.StatusBadRequest)
			return
		}
		file, ok := files[strings.ToLower(name)]
		if !ok {
			http.NotFound(writer, request)
			return
		}
		if versionText != "" {
			version, err := strconv.ParseUint(versionText, 10, 31)
			if err != nil || version != file.Version {
				http.Error(writer, "invalid CPK version", http.StatusBadRequest)
				return
			}
		}
		writer.Header().Set("Content-Type", "application/octet-stream")
		writer.Header().Set("Cache-Control", "no-store")
		allowResourceTransfer(writer, file.Size)
		http.ServeFile(writer, request, file.SourcePath)
	}
}

func cnBootstrapCPKFileList(content []byte) http.HandlerFunc {
	return func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "text/plain; charset=utf-8")
		writer.Header().Set("Cache-Control", "no-store")
		_, _ = writer.Write(content)
	}
}

func cnBootstrapCPKPatchState(content []byte) http.HandlerFunc {
	return func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "text/plain; charset=utf-8")
		writer.Header().Set("Cache-Control", "no-store")
		_, _ = writer.Write(content)
	}
}
