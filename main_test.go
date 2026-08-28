package main

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/524D/filelist2db/dataprovider"
	"github.com/524D/filelist2db/dataprovidersqlite"
)

func TestDecodeFindFilename(t *testing.T) {
	computer, basePath, ts, err := decodeFindFilename(`testdata/_computername_E%3A_20220301-134000.lst`)
	if err != nil {
		t.Fatalf("decodeFindFilename returned error: %v", err)
	}
	if computer != "computername" {
		t.Fatalf("computer name mismatch: got %q want %q", computer, "computername")
	}
	if basePath != "E:" {
		t.Fatalf("base path mismatch: got %q want %q", basePath, "E:")
	}
	want := time.Date(2022, 3, 1, 13, 40, 0, 0, time.UTC).Unix()
	if ts != want {
		t.Fatalf("timestamp mismatch: got %d want %d", ts, want)
	}
}

func TestSetSourceInfoRemovesPreviousSourceData(t *testing.T) {
	dbFile := filepath.Join(t.TempDir(), "db.sqlite")
	d, err := dataprovidersqlite.InitDataProviderSqlite(dbFile)
	if err != nil {
		t.Fatalf("InitDataProviderSqlite returned error: %v", err)
	}
	defer d.Finalize()

	if err := d.SetSourceInfo("computername", "E:", 1000); err != nil {
		t.Fatalf("SetSourceInfo initial call returned error: %v", err)
	}
	if err := d.AddFile(dataprovider.FileInfo{Path: "folder/old.txt", Size: 42, Mtime: 100, Atime: 200, Uid: 7}); err != nil {
		t.Fatalf("AddFile old file returned error: %v", err)
	}

	if err := d.SetSourceInfo("computername", "E:", 2000); err != nil {
		t.Fatalf("SetSourceInfo second call returned error: %v", err)
	}

	db, err := sql.Open("sqlite", dbFile)
	if err != nil {
		t.Fatalf("sql.Open returned error: %v", err)
	}
	defer db.Close()

	var fileCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM file2`).Scan(&fileCount); err != nil {
		t.Fatalf("COUNT(*) query returned error: %v", err)
	}
	if fileCount != 0 {
		t.Fatalf("expected previous source files to be removed, got %d remaining", fileCount)
	}
}

func TestParseFileInfoLine(t *testing.T) {
	line := "123\t1000\t7\t1\t1646112000.0\t1646112001.0\t1646112002.0\tfolder/sub/file.txt"
	f, ok := parseFileInfoLine(line)
	if !ok {
		t.Fatal("parseFileInfoLine should accept a valid file list line")
	}
	if f.Size != 123 {
		t.Fatalf("size mismatch: got %d want %d", f.Size, 123)
	}
	if f.Uid != dataprovider.UidT(1000) {
		t.Fatalf("uid mismatch: got %d want %d", f.Uid, dataprovider.UidT(1000))
	}
	if f.Mtime != 1646112000 {
		t.Fatalf("mtime mismatch: got %d want %d", f.Mtime, 1646112000)
	}
	if f.Path != "folder/sub/file.txt" {
		t.Fatalf("path mismatch: got %q want %q", f.Path, "folder/sub/file.txt")
	}
}

func TestAddFileStoresPathFragments(t *testing.T) {
	dbFile := filepath.Join(t.TempDir(), "db.sqlite")
	d, err := dataprovidersqlite.InitDataProviderSqlite(dbFile)
	if err != nil {
		t.Fatalf("InitDataProviderSqlite returned error: %v", err)
	}
	defer d.Finalize()

	if err := d.SetSourceInfo("computername", "E:", 1000); err != nil {
		t.Fatalf("SetSourceInfo returned error: %v", err)
	}
	if err := d.AddFile(dataprovider.FileInfo{Path: "folder/sub/My-Report.txt", Size: 42, Mtime: 100, Atime: 200, Uid: 7}); err != nil {
		t.Fatalf("AddFile returned error: %v", err)
	}
	if err := d.SetSourceInfo("computername", "E:", 1001); err != nil {
		t.Fatalf("SetSourceInfo repeat call returned error: %v", err)
	}
	if err := d.AddFile(dataprovider.FileInfo{Path: "folder/sub/My-Report.txt", Size: 42, Mtime: 100, Atime: 200, Uid: 7}); err != nil {
		t.Fatalf("AddFile second import returned error: %v", err)
	}

}

func TestRebuildDirTableAggregatesPerBatch(t *testing.T) {
	dbFile := filepath.Join(t.TempDir(), "db.sqlite")
	d, err := dataprovidersqlite.InitDataProviderSqlite(dbFile)
	if err != nil {
		t.Fatalf("InitDataProviderSqlite returned error: %v", err)
	}
	defer d.Finalize()

	if err := d.SetSourceInfo("computername", "E:", 1000); err != nil {
		t.Fatalf("SetSourceInfo returned error: %v", err)
	}
	for _, f := range []dataprovider.FileInfo{
		{Path: "folder/a.txt", Size: 10, Mtime: 500, Atime: 200, Uid: 1},
		{Path: "folder/b.txt", Size: 20, Mtime: 600, Atime: 300, Uid: 1},
		{Path: "folder/c.txt", Size: 30, Mtime: 700, Atime: 400, Uid: 1},
	} {
		if err := d.AddFile(f); err != nil {
			t.Fatalf("AddFile returned error: %v", err)
		}
	}

	var lastCurrent int64
	if err := d.RebuildDirTable(2, func(current, total int64) {
		lastCurrent = current
		if total <= 0 {
			t.Fatalf("progress total should be positive")
		}
	}); err != nil {
		t.Fatalf("RebuildDirTable returned error: %v", err)
	}
	if lastCurrent != 3 {
		t.Fatalf("progress callback should reach the final file count, got %d want %d", lastCurrent, 3)
	}

	db, err := sql.Open("sqlite", dbFile)
	if err != nil {
		t.Fatalf("sql.Open returned error: %v", err)
	}
	defer db.Close()

	var fileCount, totalSize int64
	if err := db.QueryRow(`
		SELECT d.file_count, d.total_size
		FROM dir d
		JOIN path p ON p.id = d.path_id
		JOIN path_elem pe ON pe.id = p.path_elem_id
		WHERE pe.elem = 'folder' AND p.is_dir = 1
	`).Scan(&fileCount, &totalSize); err != nil {
		t.Fatalf("query dir aggregate returned error: %v", err)
	}
	if fileCount != 3 {
		t.Fatalf("folder file_count mismatch: got %d want %d", fileCount, 3)
	}
	if totalSize != 60 {
		t.Fatalf("folder total_size mismatch: got %d want %d", totalSize, 60)
	}
}
