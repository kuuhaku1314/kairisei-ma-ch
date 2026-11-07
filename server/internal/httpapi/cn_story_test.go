package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"kairisei.local/server/internal/gamestate"
)

func TestCNMainStoryMenuFlagAndProgress(t *testing.T) {
	episodeIDs := []int{100001, 100002, 100003, 100004, 100101, 100102, 100103}
	api := &API{account: testAccount(t, func(state *gamestate.State) {
		state.Story.CNMainParts = []gamestate.StoryMainPart{{
			StoryMainPartID: 1,
			Sections:        []gamestate.StoryMainSection{{StoryMainSectionID: 100}, {StoryMainSectionID: 101}},
		}}
		sections := state.Story.CNMainParts[0].Sections
		for i, id := range episodeIDs {
			section := &sections[0]
			if id >= 100101 {
				section = &sections[1]
			}
			flag := 4
			if i == 0 {
				flag = 1
			}
			section.Stories = append(section.Stories, gamestate.StoryMain{StoryMainID: id, StateFlag: flag})
		}
		state.StoryRewardPolicy.MainFirstClear = gamestate.Reward{Type: 4, Num: 5}
	})}

	call := func(handler http.HandlerFunc, body string, want int8) string {
		t.Helper()
		res := httptest.NewRecorder()
		handler.ServeHTTP(res, httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body)))
		line, rest, _ := strings.Cut(res.Body.String(), "\n")
		var common commonResponse
		if res.Code != http.StatusOK || json.Unmarshal([]byte(line), &common) != nil ||
			len(common.Notifications) != 1 || common.Notifications[0].IsStoryMainCN != want {
			t.Fatalf("CN story menu flag should be %#x: status=%d, response=%s", want, res.Code, line)
		}
		method, _, _ := strings.Cut(rest, "\n")
		return method
	}

	checkShow := func(cleared int) {
		t.Helper()
		want := episodeIDs
		if cleared < len(episodeIDs) {
			want = episodeIDs[:cleared+1]
		}
		body := call(api.storyMainShow, `{"cn_story":1}`, 0x10)
		var method struct{ Parts []gamestate.StoryMainPart }
		if err := json.Unmarshal([]byte(body), &method); err != nil {
			t.Fatal(err)
		}
		var got []int
		for _, part := range method.Parts {
			for _, section := range part.Sections {
				if len(section.Stories) == 0 {
					t.Fatalf("empty chapter %d after %d clears", section.StoryMainSectionID, cleared)
				}
				for _, story := range section.Stories {
					wantFlag := 2
					if len(got) == cleared {
						wantFlag = 1
					}
					if story.StateFlag&7 != wantFlag {
						t.Fatalf("episode %d flag=%d want=%d after %d clears", story.StoryMainID, story.StateFlag, wantFlag, cleared)
					}
					got = append(got, story.StoryMainID)
				}
			}
		}
		if !slices.Equal(got, want) {
			t.Fatalf("episodes after %d clears: got %v want %v", cleared, got, want)
		}
	}

	res := httptest.NewRecorder()
	api.storyMainStart(res, httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"story_mainid":100101}`)))
	if res.Code != http.StatusBadRequest {
		t.Fatalf("chapter 101 should reject early play: %d", res.Code)
	}
	for i, id := range episodeIDs {
		checkShow(i)
		call(api.storyMainStart, fmt.Sprintf(`{"story_mainid":%d}`, id), 0x10)
		call(api.storyMainEnd, `{"is_clear":1}`, 0x10)
	}
	checkShow(len(episodeIDs))

	withoutCN := &API{account: testAccount(t, func(state *gamestate.State) {
		state.Story.CNMainParts = nil
	})}
	call(withoutCN.storyMainShow, `{"cn_story":1}`, 0)
}
