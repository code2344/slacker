package emojicache

import (
	"os"
	"testing"
)

func TestSaveAndLoad(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	want := map[string]string{"party": "https://example.test/party.png", "celebrate": "alias:party"}
	if err := Save("T123", want); err != nil {
		t.Fatal(err)
	}
	got := Load("T123")
	if got["party"] != want["party"] || got["celebrate"] != want["celebrate"] {
		t.Fatalf("Load() = %#v, want %#v", got, want)
	}
	if info, err := os.Stat(path("T123")); err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("cache permissions = %v, %v", info, err)
	}
}
