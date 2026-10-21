package legacy

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/azazo1/ecnu-dorm-watt/internal/store"
)

func TestImportLegacyRoomAndArchive(t *testing.T) {
	root := t.TempDir()
	dataDir := filepath.Join(root, "data")
	configDir := filepath.Join(root, "config")
	roomNo := "4408_MH_83_257"
	roomDir := filepath.Join(dataDir, "rooms", roomNo)
	archiveDir := filepath.Join(roomDir, "archives")
	for _, dir := range []string{configDir, roomDir, archiveDir} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	write := func(path, content string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(configDir, "room.toml"), "roomNo = \""+roomNo+"\"\n"+
		"elcarea = 102\n"+
		"elcbuis = \"new-83_MH\"\n")
	csv := "2026-01-24T14:35:32+08:00,33.43\n2026-01-25T00:00:00+08:00,10.00\n"
	write(filepath.Join(roomDir, "records.csv"), csv)
	write(filepath.Join(archiveDir, "test.csv"), csv)
	write(filepath.Join(archiveDir, "test.toml"),
		"start_time = 2026-01-24T14:35:32+08:00\n"+
			"end_time = 2026-01-25T00:00:00+08:00\n"+
			"archive_name = \"test\"\n"+
			"records_num = 2\n")

	db, err := store.Open(context.Background(), store.Options{Path: filepath.Join(root, "dormwatt.db")})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	result, err := Import(context.Background(), db, Options{DataDir: dataDir, ConfigDir: configDir})
	if err != nil {
		t.Fatal(err)
	}
	if result.Rooms != 1 || result.Samples != 2 || result.Archives != 1 {
		t.Fatalf("unexpected result: %+v", result)
	}
	stats, err := db.Stats(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if stats.Samples != 0 || stats.Archives != 1 {
		t.Fatalf("unexpected stats: %+v", stats)
	}
	if _, err := Import(context.Background(), db, Options{DataDir: dataDir, ConfigDir: configDir}); err == nil {
		t.Fatal("second import should fail on non-empty database")
	}
	_ = time.Now
}
