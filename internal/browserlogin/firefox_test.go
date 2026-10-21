package browserlogin

import (
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/golang/snappy"
)

func TestFirefoxTokens(t *testing.T) {
	for _, compressed := range []bool{false, true} {
		t.Run(map[bool]string{false: "plain", true: "snappy"}[compressed], func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "data.sqlite")
			db, err := sql.Open("sqlite", path)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := db.Exec(`CREATE TABLE data(key TEXT PRIMARY KEY, compression_type INTEGER NOT NULL, value BLOB NOT NULL)`); err != nil {
				t.Fatal(err)
			}
			value := []byte(`{"teams":{"T123":{"token":"xoxc-test"}}}`)
			compression := 0
			if compressed {
				compression = 1
				value = snappy.Encode(nil, value)
			}
			if _, err := db.Exec(`INSERT INTO data(key, compression_type, value) VALUES('localConfig_v2', ?, ?)`, compression, value); err != nil {
				t.Fatal(err)
			}
			if err := db.Close(); err != nil {
				t.Fatal(err)
			}
			got, err := firefoxTokens(path)
			if err != nil {
				t.Fatal(err)
			}
			if got["T123"] != "xoxc-test" {
				t.Fatalf("unexpected tokens: %#v", got)
			}
		})
	}
}

func TestFirefoxTokensReadsLiveWAL(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data.sqlite")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`PRAGMA journal_mode=WAL`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`PRAGMA wal_autocheckpoint=0`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE data(key TEXT PRIMARY KEY, compression_type INTEGER NOT NULL, value BLOB NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`PRAGMA wal_checkpoint(TRUNCATE)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO data(key, compression_type, value) VALUES('localConfig_v2', 0, ?)`, []byte(`{"teams":{"T123":{"token":"xoxc-from-wal"}}}`)); err != nil {
		t.Fatal(err)
	}

	got, err := firefoxTokens(path)
	if err != nil {
		t.Fatal(err)
	}
	if got["T123"] != "xoxc-from-wal" {
		t.Fatalf("unexpected tokens: %#v", got)
	}
}

func TestProfileFromPath(t *testing.T) {
	got := profileFromPath(`/Users/test/Library/Application Support/Firefox/Profiles/abc.default-release/storage/default/https+++app.slack.com/ls/data.sqlite`)
	if got != "abc.default-release" {
		t.Fatalf("profile = %q", got)
	}
}
