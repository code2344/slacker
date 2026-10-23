package emojicache

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/code2344/slacker/internal/consts"
)

// Load returns the last workspace emoji index without delaying startup on Slack.
func Load(workspaceKey string) map[string]string {
	data, err := os.ReadFile(path(workspaceKey))
	if err != nil {
		return map[string]string{}
	}
	var emojis map[string]string
	if json.Unmarshal(data, &emojis) != nil || emojis == nil {
		return map[string]string{}
	}
	return emojis
}

// Save atomically persists workspace emoji names, aliases, and image URLs.
func Save(workspaceKey string, emojis map[string]string) error {
	data, err := json.Marshal(emojis)
	if err != nil {
		return err
	}
	target := path(workspaceKey)
	temporary := target + ".tmp"
	if err := os.WriteFile(temporary, data, 0600); err != nil {
		return err
	}
	return os.Rename(temporary, target)
}

func path(workspaceKey string) string {
	sum := sha256.Sum256([]byte(workspaceKey))
	return filepath.Join(consts.CacheDir(), "emoji-"+hex.EncodeToString(sum[:8])+".json")
}
