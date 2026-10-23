package usercache

import (
	"os"
	"testing"

	"github.com/code2344/slacker/internal/slack"
)

func TestSaveAndLoadUsers(t *testing.T) {
	key := "test-workspace-" + t.Name()
	t.Cleanup(func() { _ = os.Remove(path(key)) })
	user := slack.User{ID: "U1", Name: "ruben"}
	user.Profile.DisplayName = "Ruben"
	if err := Save(key, map[string]slack.User{user.ID: user}); err != nil {
		t.Fatal(err)
	}
	loaded := Load(key)
	if loaded["U1"].Profile.DisplayName != "Ruben" || !loaded["U1"].ProfileLoaded {
		t.Fatalf("loaded users = %#v", loaded)
	}
}
