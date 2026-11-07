package cnbootstrap

import (
	"bytes"
	"context"
	"encoding/json"
	"kairisei.local/server/internal/accountstore"
	"kairisei.local/server/internal/httpapi"
	"kairisei.local/server/internal/testfixture"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func adaptCNCardShow2Response(content []byte) ([]byte, error) {
	return adaptCNProtocolMethod(content, func(method map[string]json.RawMessage) (any, error) {
		cards, err := remapJSONArray(method["0"], cnCardInfoFieldMap)
		if err != nil {
			return nil, err
		}
		return map[string]any{
			"0": int64(0),
			"1": cards,
			"2": rawOrEmptyArray(method["1"]),
			"3": rawOrEmptyArray(method["2"]),
			"4": rawOrEmptyArray(method["3"]),
			"5": rawOrEmptyArray(method["4"]),
			"6": rawOrZero(method["5"]),
		}, nil
	})
}

func BenchmarkCardShowProjection(b *testing.B) {
	view := httpapi.CardShowView{Cards: make([]httpapi.CardView, 2000)}
	for i := range view.Cards {
		view.Cards[i] = httpapi.CardView{UniqueID: int64(i + 1), CardID: 123, Level: 60, Fame: 90, SkillLevels: []int16{1, 2}, Passive: []any{}}
	}
	b.Run("encoded_adapter", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			method, err := json.Marshal(view)
			if err != nil {
				b.Fatal(err)
			}
			body := append([]byte("{\"res_code\":0}\n"), method...)
			body = append(body, []byte("\n{}")...)
			if _, err := adaptCNCardShow2Response(body); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("typed_projection", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			method, err := projectCNCardShow(view)
			if err != nil {
				b.Fatal(err)
			}
			if _, err := json.Marshal(method); err != nil {
				b.Fatal(err)
			}
		}
	})
}

func TestCNCardShowUsesTypedProjection(t *testing.T) {
	state := testfixture.RuntimeState(t)
	handler, err := httpapi.New(httpapi.Config{InitialState: state, BaseURL: "http://127.0.0.1", CardShowProjection: projectCNCardShow})
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodPost, "/CardShow2", strings.NewReader("test-session-00000000{}"))
	r = r.WithContext(context.WithValue(r.Context(), cnAuthenticatedUserKey{}, accountstore.PrimaryUserID))
	w := httptest.NewRecorder()
	cnBootstrapCardShow2(handler)(w, r)
	segments := bytes.Split(w.Body.Bytes(), []byte{'\n'})
	if w.Code != 200 || len(segments) != 3 {
		t.Fatal("card response framing changed", w.Code, w.Body.String())
	}
	var method struct {
		Time  int64            `json:"0"`
		Cards []cnCardShowInfo `json:"1"`
	}
	if err := json.Unmarshal(segments[1], &method); err != nil || len(method.Cards) != len(state.Cards) || method.Cards[0].UniqueID != state.Cards[0].UniqueID {
		t.Fatal("direct CN projection missing", err)
	}
}

func TestCardShowTypedProjectionPreservesWireContract(t *testing.T) {
	var view httpapi.CardShowView
	if err := json.Unmarshal([]byte(`{"0":[{"0":9223372036,"1":123,"6":[1,2],"7":901,"10":904,"11":[{"hp":101,"atkp":102,"intp":103,"mndp":104}],"12":[{"summonsid":17,"hp":201,"atkp":202,"intp":203,"mndp":204,"defp":205,"mdefp":206}],"13":1301,"19":90,"20":2,"21":[{"hp":701,"atkp":702}]}]}`), &view); err != nil {
		t.Fatal(err)
	}
	for _, current := range []httpapi.CardShowView{view, {}} {
		legacy, err := json.Marshal(current)
		if err != nil {
			t.Fatal(err)
		}
		legacy = append(append([]byte("{\"res_code\":0}\n"), legacy...), []byte("\n{}")...)
		converted, err := adaptCNCardShow2Response(legacy)
		if err != nil {
			t.Fatal(err)
		}
		projected, err := projectCNCardShow(current)
		if err != nil {
			t.Fatal(err)
		}
		direct, err := json.Marshal(projected)
		if err != nil {
			t.Fatal(err)
		}
		var old, next any
		decode := func(data []byte, target *any) {
			decoder := json.NewDecoder(bytes.NewReader(data))
			decoder.UseNumber()
			if err := decoder.Decode(target); err != nil {
				t.Fatal(err)
			}
		}
		decode(bytes.Split(converted, []byte{'\n'})[1], &old)
		decode(direct, &next)
		oldJSON, _ := json.Marshal(old)
		nextJSON, _ := json.Marshal(next)
		if !bytes.Equal(oldJSON, nextJSON) {
			t.Fatalf("CN wire changed\nold: %s\nnew: %s", oldJSON, nextJSON)
		}
	}
}
