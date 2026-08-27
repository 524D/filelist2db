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
