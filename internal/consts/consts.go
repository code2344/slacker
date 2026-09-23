package consts

import (
	"log/slog"
	"os"
	"path/filepath"
	"sync"
)

const Name = "slacker"

var cacheDir = sync.OnceValue(func() string {
	userCacheDir, err := os.UserCacheDir()
	if err != nil {
		userCacheDir = os.TempDir()
		slog.Warn("failed to get user cache dir; falling back to temp dir", "err", err, "path", userCacheDir)
	}

	path := filepath.Join(userCacheDir, Name)
	if err := os.MkdirAll(path, os.ModePerm); err != nil {
		slog.Error("failed to create cache dir", "err", err, "path", path)
	}

	return path
})

func CacheDir() string {
	return cacheDir()
}
