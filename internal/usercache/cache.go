package usercache

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/code2344/slacker/internal/consts"
	"github.com/code2344/slacker/internal/slack"
)

func path(key string) string {
	digest := sha256.Sum256([]byte(key))
	return filepath.Join(consts.CacheDir(), "users-"+hex.EncodeToString(digest[:8])+".json")
}

func Load(key string) map[string]slack.User {
	users := map[string]slack.User{}
	data, err := os.ReadFile(path(key))
	if err != nil || json.Unmarshal(data, &users) != nil {
		return users
	}
	for id, user := range users {
		user.ProfileLoaded = true
		users[id] = user
	}
	return users
}

func Save(key string, users map[string]slack.User) error {
	data, err := json.Marshal(users)
	if err != nil {
		return err
	}
	target := path(key)
	temporary := target + ".tmp"
	if err := os.WriteFile(temporary, data, 0600); err != nil {
		return err
	}
	_ = os.Remove(target)
	return os.Rename(temporary, target)
}
