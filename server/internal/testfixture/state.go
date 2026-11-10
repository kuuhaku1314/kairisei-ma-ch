package testfixture

import (
	"os"
	"path/filepath"
	"testing"

	"kairisei.local/server/internal/accountstore"
	"kairisei.local/server/internal/gamestate"
	"kairisei.local/server/internal/masterdata"
)

// RuntimeState prepares the existing small protocol fixtures through the same
// master-data loaders used at startup. Callers can then customize public state.
func RuntimeState(t *testing.T) gamestate.State {
	t.Helper()
	root := t.TempDir()
	state, err := accountstore.LoadSaveState(WriteTestSave(t, root))
	if err != nil {
		t.Fatal(err)
	}
	card, err := masterdata.LoadCardRuntimeMaster(WriteTestCardMaster(t, root))
	if err != nil {
		t.Fatal(err)
	}
	if err = masterdata.ApplyCardRuntimeMaster(&state, card); err != nil {
		t.Fatal(err)
	}
	item, err := masterdata.LoadItemRuntimeMaster(WriteTestItemMaster(t, root))
	if err != nil {
		t.Fatal(err)
	}
	if err = masterdata.ApplyItemRuntimeMaster(&state, item); err != nil {
		t.Fatal(err)
	}
	avatar, err := masterdata.LoadAvatarRuntimeMaster(WriteTestAvatarMaster(t, root))
	if err != nil {
		t.Fatal(err)
	}
	if err = masterdata.ApplyAvatarRuntimeMaster(&state, avatar); err != nil {
		t.Fatal(err)
	}
	navi, err := masterdata.LoadNaviRuntimeMaster(WriteTestNaviMaster(t, root))
	if err != nil {
		t.Fatal(err)
	}
	if err = masterdata.ApplyNaviRuntimeMaster(&state, navi); err != nil {
		t.Fatal(err)
	}
	stamps, err := masterdata.LoadStampRuntimeMaster(WriteTestStampMaster(t, root))
	if err != nil {
		t.Fatal(err)
	}
	if err = masterdata.ApplyStampRuntimeMaster(&state, stamps); err != nil {
		t.Fatal(err)
	}
	honors, err := masterdata.LoadHonorRuntimeMaster(WriteTestHonorMaster(t, root))
	if err != nil {
		t.Fatal(err)
	}
	if err = masterdata.ApplyHonorRuntimeMaster(&state, honors); err != nil {
		t.Fatal(err)
	}
	login, err := masterdata.LoadLoginBonusRuntimeMaster("../../config/cn602-login-bonus-runtime.json")
	if err != nil {
		t.Fatal(err)
	}
	if err = masterdata.ApplyLoginBonusRuntimeMaster(&state, login); err != nil {
		t.Fatal(err)
	}
	state.Onboarding.Step = gamestate.OnboardingStepCount
	return state
}

func WriteCPKVersions(t *testing.T, root string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, "versions.json"), []byte(`{"versions":[]}`), 0600); err != nil {
		t.Fatal(err)
	}
}
