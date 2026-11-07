package cnbootstrap

import (
	"bytes"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"kairisei.local/server/internal/wirecompression"
)

func TestBusinessCompressionPreservesProtocol(t *testing.T) {
	body := []byte("<session>\n" + strings.Repeat(`{"card":"繁體・日本語","id":12345}`, 300))
	packed, _ := wirecompression.Compress(body)
	called := 0
	handler := cnNetworkCompression(slog.New(slog.NewTextHandler(io.Discard, nil)))(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called++
		actual, _ := io.ReadAll(r.Body)
		if !bytes.Equal(actual, body) || r.Header.Get("Content-Encoding") != "" {
			t.Error("business handler did not receive original request")
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_, _ = w.Write(actual)
	}))
	for _, accept := range []string{"gzip", "gzip;q=0, *;q=1", "identity", "br, GZip;q=0.5"} {
		r := httptest.NewRequest("POST", "http://localhost/CardShow2", bytes.NewReader(packed))
		r.Header.Set("Content-Encoding", "gzip")
		r.Header.Set("Accept-Encoding", accept)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		actual := w.Body.Bytes()
		if w.Header().Get("Content-Encoding") == "gzip" {
			var err error
			actual, err = wirecompression.Decompress(actual, maxCapturedBody)
			if err != nil {
				t.Fatal(err)
			}
		}
		if w.Code != 200 || !bytes.Equal(actual, body) || (w.Header().Get("Content-Encoding") == "gzip") != acceptsGZIP(accept) {
			t.Fatal("response differs after negotiation", accept)
		}
	}
	before := called
	broken := append([]byte(nil), packed...)
	broken[len(broken)-8] ^= 1
	for _, bad := range [][]byte{broken, packed[:len(packed)-1], append(append([]byte(nil), packed...), packed...)} {
		r := httptest.NewRequest("POST", "http://localhost/TeamBattleSoloEnd", bytes.NewReader(bad))
		r.Header.Set("Content-Encoding", "gzip")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != 400 || called != before {
			t.Fatal("corrupt body reached mutation handler")
		}
	}
	for _, path := range []string{"/local/resources/file.dat", "/local/content/a", "/local/account/login", "/local/client/login"} {
		if businessCompressionRoute(httptest.NewRequest("POST", path, nil)) {
			t.Fatal("non-business route included", path)
		}
	}
}
