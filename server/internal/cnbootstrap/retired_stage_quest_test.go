package cnbootstrap

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRetiredClientCatalogsDoNotReplaceModernMainQuest(t *testing.T) {
	root := t.TempDir()
	logPath := filepath.Join(root, "requests.jsonl")
	if err := os.WriteFile(logPath, nil, 0600); err != nil {
		t.Fatal(err)
	}
	handler := newTestHandler(t, root, logPath)
	for _, endpoint := range []struct {
		path string
		body string
	}{
		{"/StageQuestShow", `{"areaid":100001,"is_solo":1}`},
		{"/GetRecommendCardInfo", ""},
		{"/GetURCardNoGetFromCurrentGaCha", ""},
	} {
		retired := httptest.NewRecorder()
		handler.ServeHTTP(retired, httptest.NewRequest(http.MethodPost, "http://local"+endpoint.path,
			strings.NewReader(cn602LocalSession+endpoint.body)))
		if retired.Code != http.StatusNotImplemented {
			t.Fatalf("retired client catalog %s status=%d", endpoint.path, retired.Code)
		}
	}
	modern := httptest.NewRecorder()
	handler.ServeHTTP(modern, httptest.NewRequest(http.MethodPost, "http://local/MainQuestShow",
		strings.NewReader(cn602LocalSession)))
	if modern.Code != http.StatusOK || !strings.Contains(modern.Body.String(), `"sections"`) {
		t.Fatalf("modern main directory status=%d body=%q", modern.Code, modern.Body.String())
	}
}
