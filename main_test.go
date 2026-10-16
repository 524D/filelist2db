package main

import (
	"context"
	"database/sql"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/524D/filelist2db/datafiller"
	"github.com/524D/filelist2db/datafillersqlite"
	"github.com/524D/filelist2db/dataprovider"
	"github.com/524D/filelist2db/dataprovidersqlite"
)

var _ dataprovider.DataProvider = (*dataprovidersqlite.DataProviderSqlite)(nil)

func TestDataProviderSearchBySimpleNameInterface(t *testing.T) {
	var provider dataprovider.DataProvider
	if provider == nil {
		_ = provider
	}
	_ = func(ctx context.Context, name string, limit int) ([]dataprovider.SearchResult, map[string]interface{}, error) {
		return provider.SearchBySimpleName(ctx, name, limit)
	}
	_ = func(ctx context.Context, source string, dir string, binIndex int, first int64, limit int64) ([]dataprovider.SearchResult, int64, error) {
		return provider.FilesInSizeBin(ctx, source, dir, binIndex, first, limit)
	}
	_ = func(ctx context.Context, source string, dir string, accessTime bool, binIndex int, first int64, limit int64) ([]dataprovider.SearchResult, int64, error) {
		return provider.FilesInAgeBin(ctx, source, dir, accessTime, binIndex, first, limit)
	}
	_ = func(ctx context.Context, minSize uint64) ([]dataprovider.SameFiles, error) {
		return provider.FindSameFiles(ctx, minSize)
	}
}

func TestExtensions(t *testing.T) {
	dbFile := filepath.Join(t.TempDir(), "db.sqlite")
	f, err := datafillersqlite.InitDataFillerSqlite(dbFile)
	if err != nil {
		t.Fatalf("InitDataFillerSqlite returned error: %v", err)
	}
	for _, src := range []string{"computername", "other"} {
		if err := f.SetSourceInfo(src, "E:", 1000); err != nil {
			t.Fatalf("SetSourceInfo returned error: %v", err)
		}
		files := []dataprovider.FileInfo{
			{Path: "A.TXT", Size: 10},
			{Path: "sub/b.txt", Size: 20},
			{Path: "sub/c.bin", Size: 5},
			{Path: "noext", Size: 1},
			{Path: ".hidden", Size: 2},
		}
		if src == "other" {
			files = files[:1]
		}
		for _, file := range files {
			if err := f.AddFile(file); err != nil {
				t.Fatalf("AddFile returned error: %v", err)
			}
		}
	}
	if err := f.RebuildDirTable(1000, nil); err != nil {
		t.Fatalf("RebuildDirTable returned error: %v", err)
	}
	if err := f.RebuildTreeIndex(2, nil); err != nil {
		t.Fatalf("RebuildTreeIndex returned error: %v", err)
	}
	f.Finalize()

	p, err := dataprovidersqlite.InitDataProviderSqlite(dbFile)
	if err != nil {
		t.Fatalf("InitDataProviderSqlite returned error: %v", err)
	}
	defer p.Finalize()

	exts, err := p.Extensions(context.Background(), "computername", "")
	if err != nil {
		t.Fatalf("Extensions returned error: %v", err)
	}
	want := []dataprovider.ExtensionStats{
		{Ext: "txt", Size: 30, FileCount: 2},
		{Ext: "bin", Size: 5, FileCount: 1},
		{Ext: "", Size: 3, FileCount: 2},
	}
	if !reflect.DeepEqual(exts, want) {
		t.Fatalf("Extensions = %#v, want %#v", exts, want)
	}

	subExts, err := p.Extensions(context.Background(), "computername", "E:/sub")
	if err != nil {
		t.Fatalf("Extensions for E:/sub returned error: %v", err)
	}
	wantSub := []dataprovider.ExtensionStats{
		{Ext: "txt", Size: 20, FileCount: 1},
		{Ext: "bin", Size: 5, FileCount: 1},
	}
	if !reflect.DeepEqual(subExts, wantSub) {
		t.Fatalf("Extensions for E:/sub = %#v, want %#v", subExts, wantSub)
	}

	var got []string
	for first := int64(0); ; first++ {
		page, total, err := p.FilesWithExtension(context.Background(), "computername", "", ".TXT", first, 1)
		if err != nil {
			t.Fatalf("FilesWithExtension returned error: %v", err)
		}
		if total != 2 {
			t.Fatalf("FilesWithExtension total = %d, want 2", total)
		}
		if len(page) == 0 {
			break
		}
		got = append(got, page[0].Path)
	}
	if !reflect.DeepEqual(got, []string{"E:/A.TXT", "E:/sub/b.txt"}) {
		t.Fatalf("FilesWithExtension returned %v", got)
	}

	if subFiles, subTotal, err := p.FilesWithExtension(context.Background(), "computername", "E:/sub", "txt", 0, 10); err != nil || subTotal != 1 || len(subFiles) != 1 || subFiles[0].Path != "E:/sub/b.txt" {
		t.Fatalf("FilesWithExtension for E:/sub = %v, %d, %v", subFiles, subTotal, err)
	}

	if files, total, err := p.FilesWithExtension(context.Background(), "computername", "", "xyz", 0, 10); err != nil || total != 0 || len(files) != 0 {
		t.Fatalf("FilesWithExtension for unknown extension = %v, %d, %v", files, total, err)
	}
}

func TestFilesInSizeBinReturnsMatchingFilesInCurrentDirectory(t *testing.T) {
	dbFile := filepath.Join(t.TempDir(), "db.sqlite")
	f, err := datafillersqlite.InitDataFillerSqlite(dbFile)
	if err != nil {
		t.Fatalf("InitDataFillerSqlite returned error: %v", err)
	}
	if err := f.SetSourceInfo("computername", "E:", 1000); err != nil {
		t.Fatalf("SetSourceInfo returned error: %v", err)
	}
	for _, file := range []dataprovider.FileInfo{
		{Path: "A.bin", Size: 16, Mtime: 100, Atime: 200, Uid: 7},
		{Path: "B.bin", Size: 24, Mtime: 110, Atime: 210, Uid: 7},
		{Path: "sub/C.bin", Size: 30, Mtime: 120, Atime: 220, Uid: 7},
		{Path: "sub/D.bin", Size: 64, Mtime: 130, Atime: 230, Uid: 7},
		{Path: "other.bin", Size: 128, Mtime: 140, Atime: 240, Uid: 7},
	} {
		if err := f.AddFile(file); err != nil {
			t.Fatalf("AddFile returned error: %v", err)
		}
	}
	if err := f.RebuildDirTable(1000, nil); err != nil {
		t.Fatalf("RebuildDirTable returned error: %v", err)
	}
	if err := f.RebuildTreeIndex(2, nil); err != nil {
		t.Fatalf("RebuildTreeIndex returned error: %v", err)
	}
	f.Finalize()

	p, err := dataprovidersqlite.InitDataProviderSqlite(dbFile)
	if err != nil {
		t.Fatalf("InitDataProviderSqlite returned error: %v", err)
	}
	defer p.Finalize()

	files, total, err := p.FilesInSizeBin(context.Background(), "computername", "E:", 5, 0, 100)
	if err != nil {
		t.Fatalf("FilesInSizeBin returned error: %v", err)
	}
	if total != 3 {
		t.Fatalf("FilesInSizeBin total = %d, want 3", total)
	}
	if len(files) != 3 {
		t.Fatalf("FilesInSizeBin returned %d files, want 3 for the [16,31) range", len(files))
	}
	got := map[string]uint64{}
	for _, file := range files {
		got[file.Path] = file.Size
	}
	if got["E:/A.bin"] != 16 || got["E:/B.bin"] != 24 || got["E:/sub/C.bin"] != 30 {
		t.Fatalf("FilesInSizeBin returned unexpected files: %#v", got)
	}
	if _, ok := got["E:/other.bin"]; ok {
		t.Fatal("FilesInSizeBin should exclude files outside the current directory")
	}
	if _, ok := got["E:/sub/D.bin"]; ok {
		t.Fatal("FilesInSizeBin should exclude sizes outside the selected bin")
	}

	subFiles, subTotal, err := p.FilesInSizeBin(context.Background(), "computername", "E:/sub", 5, 0, 100)
	if err != nil {
		t.Fatalf("FilesInSizeBin returned error: %v", err)
	}
	if subTotal != 1 {
		t.Fatalf("FilesInSizeBin total for E:/sub = %d, want 1", subTotal)
	}
	if len(subFiles) != 1 || subFiles[0].Path != "E:/sub/C.bin" {
		t.Fatalf("FilesInSizeBin for E:/sub returned unexpected files: %#v", subFiles)
	}

	var paged []string
	for first := int64(0); ; first += 2 {
		page, pageTotal, err := p.FilesInSizeBin(context.Background(), "computername", "E:", 5, first, 2)
		if err != nil {
			t.Fatalf("FilesInSizeBin returned error: %v", err)
		}
		if pageTotal != 3 {
			t.Fatalf("page at %d total = %d, want 3 regardless of paging", first, pageTotal)
		}
		if len(page) > 2 {
			t.Fatalf("page at %d has %d files, want at most 2", first, len(page))
		}
		for _, file := range page {
			paged = append(paged, file.Path)
		}
		if len(page) < 2 {
			break
		}
	}
	if len(paged) != 3 {
		t.Fatalf("paging returned %d files, want 3: %v", len(paged), paged)
	}
	seen := map[string]bool{}
	for _, path := range paged {
		if seen[path] {
			t.Fatalf("paging returned %s twice: %v", path, paged)
		}
		seen[path] = true
	}
}

func TestFilesInAgeBinSelectsByModifyOrAccessTime(t *testing.T) {
	dbFile := filepath.Join(t.TempDir(), "db.sqlite")
	f, err := datafillersqlite.InitDataFillerSqlite(dbFile)
	if err != nil {
		t.Fatalf("InitDataFillerSqlite returned error: %v", err)
	}
	now := time.Now().Unix()
	const day = int64(24 * 3600)
	if err := f.SetSourceInfo("computername", "E:", now); err != nil {
		t.Fatalf("SetSourceInfo returned error: %v", err)
	}
	// Bin 0: < 4 months, bin 1: 4 to 12 months, bin 5: > 12 years.
	for _, file := range []dataprovider.FileInfo{
		{Path: "new.bin", Size: 1, Mtime: now - day, Atime: now - 200*day},
		{Path: "sub/mid.bin", Size: 2, Mtime: now - 200*day, Atime: now - day},
		{Path: "sub/old.bin", Size: 3, Mtime: now - 5000*day, Atime: now - 5000*day},
	} {
		if err := f.AddFile(file); err != nil {
			t.Fatalf("AddFile returned error: %v", err)
		}
	}
	if err := f.RebuildDirTable(1000, nil); err != nil {
		t.Fatalf("RebuildDirTable returned error: %v", err)
	}
	if err := f.RebuildTreeIndex(2, nil); err != nil {
		t.Fatalf("RebuildTreeIndex returned error: %v", err)
	}
	f.Finalize()

	p, err := dataprovidersqlite.InitDataProviderSqlite(dbFile)
	if err != nil {
		t.Fatalf("InitDataProviderSqlite returned error: %v", err)
	}
	defer p.Finalize()

	for _, tc := range []struct {
		dir        string
		accessTime bool
		bin        int
		want       []string
	}{
		{"E:", false, 0, []string{"E:/new.bin"}},
		{"E:", false, 1, []string{"E:/sub/mid.bin"}},
		{"E:", false, 5, []string{"E:/sub/old.bin"}},
		{"E:", true, 0, []string{"E:/sub/mid.bin"}},
		{"E:", true, 1, []string{"E:/new.bin"}},
		{"E:/sub", false, 0, nil},
		{"E:/sub", true, 0, []string{"E:/sub/mid.bin"}},
	} {
		files, total, err := p.FilesInAgeBin(context.Background(), "computername", tc.dir, tc.accessTime, tc.bin, 0, 100)
		if err != nil {
			t.Fatalf("FilesInAgeBin returned error: %v", err)
		}
		var got []string
		for _, file := range files {
			got = append(got, file.Path)
		}
		if total != int64(len(tc.want)) || !reflect.DeepEqual(got, tc.want) {
			t.Errorf("FilesInAgeBin(%s, atime=%v, bin=%d) = %v (total %d), want %v", tc.dir, tc.accessTime, tc.bin, got, total, tc.want)
		}
	}
}

func TestDataFillerInterfaceSplit(t *testing.T) {
	var provider dataprovider.DataProvider
	var filler datafiller.DataFiller
	_ = provider
	_ = filler

	dbFile := filepath.Join(t.TempDir(), "db.sqlite")
	f, err := datafillersqlite.InitDataFillerSqlite(dbFile)
	if err != nil {
		t.Fatalf("InitDataFillerSqlite returned error: %v", err)
	}
	defer f.Finalize()

	if err := f.SetSourceInfo("computername", "E:", 1000); err != nil {
		t.Fatalf("SetSourceInfo returned error: %v", err)
	}

	if err := f.AddFile(dataprovider.FileInfo{Path: "folder/file.txt", Size: 42, Mtime: 100, Atime: 200, Uid: 7}); err != nil {
		t.Fatalf("AddFile returned error: %v", err)
	}
}

func initFilledProvider(t *testing.T, dbFile, source, basePath string, acqTime int64, files ...dataprovider.FileInfo) dataprovider.DataProvider {
	t.Helper()

	f, err := datafillersqlite.InitDataFillerSqlite(dbFile)
	if err != nil {
		t.Fatalf("InitDataFillerSqlite returned error: %v", err)
	}
	defer f.Finalize()

	if err := f.SetSourceInfo(source, basePath, acqTime); err != nil {
		t.Fatalf("SetSourceInfo returned error: %v", err)
	}
	for _, file := range files {
		if err := f.AddFile(file); err != nil {
			t.Fatalf("AddFile returned error: %v", err)
		}
	}

	p, err := dataprovidersqlite.InitDataProviderSqlite(dbFile)
	if err != nil {
		t.Fatalf("InitDataProviderSqlite returned error: %v", err)
	}
	return p
}

func TestDirInfoIncludesAcquisitionTimeRange(t *testing.T) {
	dbFile := filepath.Join(t.TempDir(), "db.sqlite")
	f, err := datafillersqlite.InitDataFillerSqlite(dbFile)
	if err != nil {
		t.Fatalf("InitDataFillerSqlite returned error: %v", err)
	}
	if err := f.SetSourceInfo("computername", "E:", 1000); err != nil {
		t.Fatalf("SetSourceInfo returned error: %v", err)
	}
	if err := f.AddFile(dataprovider.FileInfo{Path: "old.txt", Size: 10, Mtime: 500, Atime: 200, Uid: 7}); err != nil {
		t.Fatalf("AddFile old file returned error: %v", err)
	}
	if err := f.AddFile(dataprovider.FileInfo{Path: "new.txt", Size: 20, Mtime: 600, Atime: 300, Uid: 7}); err != nil {
		t.Fatalf("AddFile new file returned error: %v", err)
	}
	if err := f.RebuildDirTable(1000, nil); err != nil {
		t.Fatalf("RebuildDirTable returned error: %v", err)
	}
	f.Finalize()

	p, err := dataprovidersqlite.InitDataProviderSqlite(dbFile)
	if err != nil {
		t.Fatalf("InitDataProviderSqlite returned error: %v", err)
	}
	defer p.Finalize()

	info, err := p.DirInfo("computername", "E:")
	if err != nil {
		t.Fatalf("DirInfo returned error: %v", err)
	}
	if info == nil {
		t.Fatal("DirInfo returned nil info")
	}
	if got, ok := info["acqTimeMin"]; !ok || got != int64(1000) {
		t.Fatalf("acqTimeMin mismatch: got %#v want 1000", got)
	}
	if got, ok := info["acqTimeMax"]; !ok || got != int64(1000) {
		t.Fatalf("acqTimeMax mismatch: got %#v want 1000", got)
	}
}

func TestDirInfoIncludesGenerationTime(t *testing.T) {
	dbFile := filepath.Join(t.TempDir(), "db.sqlite")
	f, err := datafillersqlite.InitDataFillerSqlite(dbFile)
	if err != nil {
		t.Fatalf("InitDataFillerSqlite returned error: %v", err)
	}
	if err := f.SetSourceInfo("computername", "E:", 1000); err != nil {
		t.Fatalf("SetSourceInfo returned error: %v", err)
	}
	if err := f.AddFile(dataprovider.FileInfo{Path: "a.txt", Size: 10, Mtime: 500, Atime: 200, Uid: 7}); err != nil {
		t.Fatalf("AddFile returned error: %v", err)
	}
	before := time.Now().Unix()
	if err := f.RebuildDirTable(1000, nil); err != nil {
		t.Fatalf("RebuildDirTable returned error: %v", err)
	}
	after := time.Now().Unix()
	f.Finalize()

	p, err := dataprovidersqlite.InitDataProviderSqlite(dbFile)
	if err != nil {
		t.Fatalf("InitDataProviderSqlite returned error: %v", err)
	}
	defer p.Finalize()

	info, err := p.DirInfo("computername", "E:")
	if err != nil {
		t.Fatalf("DirInfo returned error: %v", err)
	}
	got, ok := info["genTime"]
	if !ok {
		t.Fatal("genTime missing from DirInfo metadata")
	}
	genTime, ok := got.(int64)
	if !ok {
		t.Fatalf("genTime wrong type: got %T want int64", got)
	}
	if genTime < before || genTime > after {
		t.Fatalf("genTime out of range: got %d want between %d and %d", genTime, before, after)
	}
}

func TestDirInfoSubdirSizesAreNotPaddedWithZeroes(t *testing.T) {
	dbFile := filepath.Join(t.TempDir(), "db.sqlite")
	f, err := datafillersqlite.InitDataFillerSqlite(dbFile)
	if err != nil {
		t.Fatalf("InitDataFillerSqlite returned error: %v", err)
	}
	if err := f.SetSourceInfo("computername", "E:", 1000); err != nil {
		t.Fatalf("SetSourceInfo returned error: %v", err)
	}
	if err := f.AddFile(dataprovider.FileInfo{Path: "folder/file.txt", Size: 42, Mtime: 100, Atime: 200, Uid: 7}); err != nil {
		t.Fatalf("AddFile returned error: %v", err)
	}
	if err := f.AddFile(dataprovider.FileInfo{Path: "folder/other.bin", Size: 7, Mtime: 100, Atime: 200, Uid: 7}); err != nil {
		t.Fatalf("AddFile second file returned error: %v", err)
	}
	if err := f.RebuildDirTable(1000, nil); err != nil {
		t.Fatalf("RebuildDirTable returned error: %v", err)
	}
	f.Finalize()

	p, err := dataprovidersqlite.InitDataProviderSqlite(dbFile)
	if err != nil {
		t.Fatalf("InitDataProviderSqlite returned error: %v", err)
	}
	defer p.Finalize()

	info, err := p.DirInfo("computername", "E:")
	if err != nil {
		t.Fatalf("DirInfo returned error: %v", err)
	}
	if info == nil {
		t.Fatal("DirInfo returned nil info")
	}

	value := reflect.ValueOf(info["subDirs"])
	if value.Kind() != reflect.Slice || value.Len() != 1 {
		t.Fatalf("subDirs has kind %s length %d, want slice(len=1)", value.Kind(), value.Len())
	}
	first := value.Index(0)
	if first.FieldByName("Name").String() != "folder" {
		t.Fatalf("subDirs[0].Name mismatch: got %q want %q", first.FieldByName("Name").String(), "folder")
	}
	if first.FieldByName("Size").Uint() != 49 {
		t.Fatalf("subDirs[0].Size mismatch: got %d want %d", first.FieldByName("Size").Uint(), 49)
	}
	if first.FieldByName("FileCount").Int() != 2 {
		t.Fatalf("subDirs[0].FileCount mismatch: got %d want %d", first.FieldByName("FileCount").Int(), 2)
	}
}

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

func TestInitDataProviderSqlitePreparesReadOnlyStatements(t *testing.T) {
	dbFile := filepath.Join(t.TempDir(), "db.sqlite")
	filler, err := datafillersqlite.InitDataFillerSqlite(dbFile)
	if err != nil {
		t.Fatalf("InitDataFillerSqlite returned error: %v", err)
	}
	filler.Finalize()

	p, err := dataprovidersqlite.InitDataProviderSqlite(dbFile)
	if err != nil {
		t.Fatalf("InitDataProviderSqlite returned error: %v", err)
	}
	defer p.Finalize()

	v := reflect.ValueOf(p).Elem()
	for _, name := range []string{
		"stmtSelectPathElem",
		"stmtSelectPathIDByElemAndParentPathID",
		"stmtSelectRootSources",
		"stmtSelectDirSummary",
	} {
		field := v.FieldByName(name)
		if !field.IsValid() {
			t.Fatalf("missing prepared statement field %q", name)
		}
		if field.IsNil() {
			t.Fatalf("prepared statement field %q is nil", name)
		}
	}
}

func TestSetSourceInfoRecordsAcquisitionTimestamp(t *testing.T) {
	dbFile := filepath.Join(t.TempDir(), "db.sqlite")
	filler, err := datafillersqlite.InitDataFillerSqlite(dbFile)
	if err != nil {
		t.Fatalf("InitDataFillerSqlite returned error: %v", err)
	}
	if err := filler.SetSourceInfo("computername", "E:", 1000); err != nil {
		t.Fatalf("SetSourceInfo returned error: %v", err)
	}
	filler.Finalize()

	db, err := sql.Open("sqlite", dbFile)
	if err != nil {
		t.Fatalf("sql.Open returned error: %v", err)
	}
	defer db.Close()

	var lastID, ts int64
	if err := db.QueryRow(`SELECT id, timestamp FROM input_file ORDER BY id DESC LIMIT 1`).Scan(&lastID, &ts); err != nil {
		t.Fatalf("input_file query returned error: %v", err)
	}
	if lastID != 1 {
		t.Fatalf("input_file id mismatch: got %d want %d", lastID, 1)
	}
	if ts != 1000 {
		t.Fatalf("input_file timestamp mismatch: got %d want %d", ts, 1000)
	}
}

func TestSetSourceInfoRemovesPreviousSourceData(t *testing.T) {
	dbFile := filepath.Join(t.TempDir(), "db.sqlite")
	filler, err := datafillersqlite.InitDataFillerSqlite(dbFile)
	if err != nil {
		t.Fatalf("InitDataFillerSqlite returned error: %v", err)
	}
	if err := filler.SetSourceInfo("computername", "E:", 1000); err != nil {
		t.Fatalf("SetSourceInfo initial call returned error: %v", err)
	}
	if err := filler.AddFile(dataprovider.FileInfo{Path: "folder/old.txt", Size: 42, Mtime: 100, Atime: 200, Uid: 7}); err != nil {
		t.Fatalf("AddFile old file returned error: %v", err)
	}
	filler.Finalize()

	filler, err = datafillersqlite.InitDataFillerSqlite(dbFile)
	if err != nil {
		t.Fatalf("InitDataFillerSqlite second call returned error: %v", err)
	}
	if err := filler.SetSourceInfo("computername", "E:", 2000); err != nil {
		t.Fatalf("SetSourceInfo second call returned error: %v", err)
	}
	filler.Finalize()

	db, err := sql.Open("sqlite", dbFile)
	if err != nil {
		t.Fatalf("sql.Open returned error: %v", err)
	}
	defer db.Close()

	var fileCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM file`).Scan(&fileCount); err != nil {
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
	filler, err := datafillersqlite.InitDataFillerSqlite(dbFile)
	if err != nil {
		t.Fatalf("InitDataFillerSqlite returned error: %v", err)
	}
	if err := filler.SetSourceInfo("computername", "E:", 1000); err != nil {
		t.Fatalf("SetSourceInfo returned error: %v", err)
	}
	if err := filler.AddFile(dataprovider.FileInfo{Path: "folder/sub/My-Report.txt", Size: 42, Mtime: 100, Atime: 200, Uid: 7}); err != nil {
		t.Fatalf("AddFile returned error: %v", err)
	}
	if err := filler.SetSourceInfo("computername", "E:", 1001); err != nil {
		t.Fatalf("SetSourceInfo repeat call returned error: %v", err)
	}
	if err := filler.AddFile(dataprovider.FileInfo{Path: "folder/sub/My-Report.txt", Size: 42, Mtime: 100, Atime: 200, Uid: 7}); err != nil {
		t.Fatalf("AddFile second import returned error: %v", err)
	}
	filler.Finalize()
}

func TestAddFilePopulatesSimplePathTables(t *testing.T) {
	dbFile := filepath.Join(t.TempDir(), "db.sqlite")
	filler, err := datafillersqlite.InitDataFillerSqlite(dbFile)
	if err != nil {
		t.Fatalf("InitDataFillerSqlite returned error: %v", err)
	}
	if err := filler.SetSourceInfo("computername", "E:", 1000); err != nil {
		t.Fatalf("SetSourceInfo returned error: %v", err)
	}
	if err := filler.AddFile(dataprovider.FileInfo{Path: "folder/sub/My-Report.txt", Size: 42, Mtime: 100, Atime: 200, Uid: 7}); err != nil {
		t.Fatalf("AddFile returned error: %v", err)
	}
	filler.Finalize()

	db, err := sql.Open("sqlite", dbFile)
	if err != nil {
		t.Fatalf("sql.Open returned error: %v", err)
	}
	defer db.Close()

	var simpleCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM simple_path_elem`).Scan(&simpleCount); err != nil {
		t.Fatalf("simple_path_elem count query returned error: %v", err)
	}
	if simpleCount == 0 {
		t.Fatalf("simple_path_elem should contain simplified path elements")
	}

	var translateCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM simple_path_translate`).Scan(&translateCount); err != nil {
		t.Fatalf("simple_path_translate count query returned error: %v", err)
	}
	if translateCount == 0 {
		t.Fatalf("simple_path_translate should link simplified elements to path elements")
	}

	var simpleElem string
	if err := db.QueryRow(`SELECT simple_elem FROM simple_path_elem WHERE simple_elem = 'myreporttxt'`).Scan(&simpleElem); err != nil {
		t.Fatalf("expected simplified file name mapping missing: %v", err)
	}
	if simpleElem != "myreporttxt" {
		t.Fatalf("simplified element mismatch: got %q want %q", simpleElem, "myreporttxt")
	}

	var paired int
	if err := db.QueryRow(`SELECT COUNT(*) FROM simple_path_translate WHERE simple_path_elem_id = (SELECT id FROM simple_path_elem WHERE simple_elem = 'myreporttxt')`).Scan(&paired); err != nil {
		t.Fatalf("translate lookup query returned error: %v", err)
	}
	if paired == 0 {
		t.Fatalf("myreporttxt should map to at least one path_elem row")
	}
}

func TestSearchBySimpleNameFindsOriginalPathElements(t *testing.T) {
	dbFile := filepath.Join(t.TempDir(), "db.sqlite")
	p := initFilledProvider(t, dbFile, "computername", "E:", 1000,
		dataprovider.FileInfo{Path: "folder/sub/My-Report.txt", Size: 42, Mtime: 100, Atime: 200, Uid: 7},
	)
	defer p.Finalize()

	results, meta, err := p.SearchBySimpleName(context.Background(), "myreport", 10)
	if err != nil {
		t.Fatalf("SearchBySimpleName returned error: %v", err)
	}
	if len(results) == 0 {
		t.Fatalf("SearchBySimpleName should find the original file path for simplified name %q", "myreport")
	}
	if len(meta) == 0 || meta["SearchTimeMicroSeconds"] == nil {
		t.Fatalf("SearchBySimpleName metadata missing SearchTimeMicroSeconds")
	}
	if got := results[0].Path; got != "E:/folder/sub/My-Report.txt" {
		t.Fatalf("SearchBySimpleName path mismatch: got %q want %q", got, "E:/folder/sub/My-Report.txt")
	}
}

func TestSearchSimplePathOnly(t *testing.T) {
	dbFile := filepath.Join(t.TempDir(), "db.sqlite")
	p := initFilledProvider(t, dbFile, "computername", "E:", 1000,
		dataprovider.FileInfo{Path: "folder/sub/My-Report.txt", Size: 42, Mtime: 100, Atime: 200, Uid: 7},
		dataprovider.FileInfo{Path: "folder/sub/Other-Report.txt", Size: 99, Mtime: 100, Atime: 200, Uid: 7},
	)
	defer p.Finalize()

	results, meta, err := p.Search(context.Background(), dataprovider.SearchSelection{Path: "myreport", SimplePath: true, ResultsLimit: 10, Kind: -1, SizeMin: -1, SizeMax: -1, MtimeMin: -1, MtimeMax: -1, AtimeMin: -1, AtimeMax: -1})
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}
	if len(results) == 0 {
		t.Fatalf("Search should find simplified path match for %q", "myreport")
	}
	if got := results[0].Path; got != "E:/folder/sub/My-Report.txt" {
		t.Fatalf("Search path mismatch: got %q want %q", got, "E:/folder/sub/My-Report.txt")
	}
	if _, ok := meta["SearchTimeMicroSeconds"]; !ok {
		t.Fatalf("Search metadata missing SearchTimeMicroSeconds")
	}
}

func TestSearchSizeRangeOnly(t *testing.T) {
	dbFile := filepath.Join(t.TempDir(), "db.sqlite")
	p := initFilledProvider(t, dbFile, "computername", "E:", 1000,
		dataprovider.FileInfo{Path: "folder/a.txt", Size: 10, Mtime: 100, Atime: 200, Uid: 7},
		dataprovider.FileInfo{Path: "folder/b.txt", Size: 20, Mtime: 100, Atime: 200, Uid: 7},
		dataprovider.FileInfo{Path: "folder/c.txt", Size: 30, Mtime: 100, Atime: 200, Uid: 7},
	)
	defer p.Finalize()

	results, _, err := p.Search(context.Background(), dataprovider.SearchSelection{SizeMin: 15, SizeMax: 25, ResultsLimit: 10, Kind: -1, MtimeMin: -1, MtimeMax: -1, AtimeMin: -1, AtimeMax: -1})
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("Search size-range should return exactly one result, got %d", len(results))
	}
	if got := results[0].Path; got != "E:/folder/b.txt" {
		t.Fatalf("Search path mismatch: got %q want %q", got, "E:/folder/b.txt")
	}
}

func TestSimplePathTranslationIndexExists(t *testing.T) {
	dbFile := filepath.Join(t.TempDir(), "db.sqlite")
	filler, err := datafillersqlite.InitDataFillerSqlite(dbFile)
	if err != nil {
		t.Fatalf("InitDataFillerSqlite returned error: %v", err)
	}
	filler.Finalize()

	d, err := dataprovidersqlite.InitDataProviderSqlite(dbFile)
	if err != nil {
		t.Fatalf("InitDataProviderSqlite returned error: %v", err)
	}
	defer d.Finalize()

	db, err := sql.Open("sqlite", dbFile)
	if err != nil {
		t.Fatalf("sql.Open returned error: %v", err)
	}
	defer db.Close()

	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'index' AND name = 'simple_path_translate_unique_idx'`).Scan(&count); err != nil {
		t.Fatalf("sqlite_master query returned error: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected simple_path_translate_unique_idx to exist, got %d matches", count)
	}
}

func TestAddFileStoresServerShareRootAsSinglePathElement(t *testing.T) {
	dbFile := filepath.Join(t.TempDir(), "db.sqlite")
	filler, err := datafillersqlite.InitDataFillerSqlite(dbFile)
	if err != nil {
		t.Fatalf("InitDataFillerSqlite returned error: %v", err)
	}
	if err := filler.SetSourceInfo("", `\\server\share\Projects`, 1000); err != nil {
		t.Fatalf("SetSourceInfo returned error: %v", err)
	}
	if err := filler.AddFile(dataprovider.FileInfo{Path: "folder/file.txt", Size: 42, Mtime: 100, Atime: 200, Uid: 7}); err != nil {
		t.Fatalf("AddFile returned error: %v", err)
	}
	filler.Finalize()

	db, err := sql.Open("sqlite", dbFile)
	if err != nil {
		t.Fatalf("sql.Open returned error: %v", err)
	}
	defer db.Close()

	var roots []string
	rows, err := db.Query(`SELECT pe.elem FROM path p JOIN path_elem pe ON pe.id = p.path_elem_id WHERE p.parent_id = -1 ORDER BY pe.elem`)
	if err != nil {
		t.Fatalf("query root path elements returned error: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var elem string
		if err := rows.Scan(&elem); err != nil {
			t.Fatalf("scan root path element returned error: %v", err)
		}
		roots = append(roots, elem)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("row iteration error: %v", err)
	}
	if len(roots) != 1 || roots[0] != "server/share" {
		t.Fatalf("root path elements mismatch: got %v want [%q]", roots, "server/share")
	}
}

func TestDataSourcesReturnsRootNames(t *testing.T) {
	dbFile := filepath.Join(t.TempDir(), "db.sqlite")
	filler, err := datafillersqlite.InitDataFillerSqlite(dbFile)
	if err != nil {
		t.Fatalf("InitDataFillerSqlite returned error: %v", err)
	}
	if err := filler.SetSourceInfo("computername", "E:", 1000); err != nil {
		t.Fatalf("SetSourceInfo first source returned error: %v", err)
	}
	if err := filler.AddFile(dataprovider.FileInfo{Path: "folder/file1.txt", Size: 42, Mtime: 100, Atime: 200, Uid: 7}); err != nil {
		t.Fatalf("AddFile first source returned error: %v", err)
	}
	if err := filler.SetSourceInfo("", `\\server\share\Projects`, 1001); err != nil {
		t.Fatalf("SetSourceInfo second source returned error: %v", err)
	}
	if err := filler.AddFile(dataprovider.FileInfo{Path: "folder/file2.txt", Size: 42, Mtime: 100, Atime: 200, Uid: 7}); err != nil {
		t.Fatalf("AddFile second source returned error: %v", err)
	}
	filler.Finalize()

	d, err := dataprovidersqlite.InitDataProviderSqlite(dbFile)
	if err != nil {
		t.Fatalf("InitDataProviderSqlite returned error: %v", err)
	}
	defer d.Finalize()

	sources, err := d.DataSources()
	if err != nil {
		t.Fatalf("DataSources returned error: %v", err)
	}
	if len(sources) != 2 {
		t.Fatalf("DataSources length mismatch: got %d want %d (%v)", len(sources), 2, sources)
	}
	seen := map[string]bool{}
	for _, s := range sources {
		seen[s] = true
	}
	if !seen["computername"] || !seen["server/share"] {
		t.Fatalf("DataSources mismatch: got %v want roots %q and %q", sources, "computername", "server/share")
	}
}

func TestRebuildDirTableAggregatesPerBatch(t *testing.T) {
	dbFile := filepath.Join(t.TempDir(), "db.sqlite")
	filler, err := datafillersqlite.InitDataFillerSqlite(dbFile)
	if err != nil {
		t.Fatalf("InitDataFillerSqlite returned error: %v", err)
	}
	if err := filler.SetSourceInfo("computername", "E:", 1000); err != nil {
		t.Fatalf("SetSourceInfo returned error: %v", err)
	}
	for _, f := range []dataprovider.FileInfo{
		{Path: "folder/a.txt", Size: 10, Mtime: 500, Atime: 200, Uid: 1},
		{Path: "folder/b.txt", Size: 20, Mtime: 600, Atime: 300, Uid: 1},
		{Path: "folder/c.txt", Size: 30, Mtime: 700, Atime: 400, Uid: 1},
	} {
		if err := filler.AddFile(f); err != nil {
			t.Fatalf("AddFile returned error: %v", err)
		}
	}
	filler.Finalize()

	filler, err = datafillersqlite.InitDataFillerSqlite(dbFile)
	if err != nil {
		t.Fatalf("InitDataFillerSqlite second call returned error: %v", err)
	}
	var lastCurrent int64
	if err := filler.RebuildDirTable(2, func(current, total int64) {
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
	filler.Finalize()

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
		WHERE pe.elem = 'folder' AND p.node_type != 0
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

func TestRebuildDirTableStoresAcquisitionTimeRange(t *testing.T) {
	dbFile := filepath.Join(t.TempDir(), "db.sqlite")
	filler, err := datafillersqlite.InitDataFillerSqlite(dbFile)
	if err != nil {
		t.Fatalf("InitDataFillerSqlite returned error: %v", err)
	}
	if err := filler.SetSourceInfo("computername1", "E:", 1000); err != nil {
		t.Fatalf("SetSourceInfo first source returned error: %v", err)
	}
	if err := filler.AddFile(dataprovider.FileInfo{Path: "folder/old.txt", Size: 10, Mtime: 500, Atime: 200, Uid: 1}); err != nil {
		t.Fatalf("AddFile old file returned error: %v", err)
	}
	if err := filler.SetSourceInfo("computername2", "F:", 2000); err != nil {
		t.Fatalf("SetSourceInfo second source returned error: %v", err)
	}
	if err := filler.AddFile(dataprovider.FileInfo{Path: "folder/new.txt", Size: 20, Mtime: 600, Atime: 300, Uid: 1}); err != nil {
		t.Fatalf("AddFile new file returned error: %v", err)
	}
	if err := filler.RebuildDirTable(2, nil); err != nil {
		t.Fatalf("RebuildDirTable returned error: %v", err)
	}
	filler.Finalize()

	db, err := sql.Open("sqlite", dbFile)
	if err != nil {
		t.Fatalf("sql.Open returned error: %v", err)
	}
	defer db.Close()

	rows, err := db.Query(`
		SELECT d.acqtime_min, d.acqtime_max
		FROM dir d
		JOIN path p ON p.id = d.path_id
		JOIN path_elem pe ON pe.id = p.path_elem_id
		WHERE pe.elem = 'folder' AND p.node_type != 0
		ORDER BY d.acqtime_min
	`)
	if err != nil {
		t.Fatalf("query dir acquisition ranges returned error: %v", err)
	}
	defer rows.Close()

	var seen []struct{ min, max int64 }
	for rows.Next() {
		var minAcq, maxAcq int64
		if err := rows.Scan(&minAcq, &maxAcq); err != nil {
			t.Fatalf("scan dir acquisition range returned error: %v", err)
		}
		seen = append(seen, struct{ min, max int64 }{min: minAcq, max: maxAcq})
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("row iteration error: %v", err)
	}
	if len(seen) != 2 {
		t.Fatalf("expected 2 folder rows, got %d: %#v", len(seen), seen)
	}
	if seen[0].min != 1000 || seen[0].max != 1000 {
		t.Fatalf("first folder acquisition range mismatch: got min=%d max=%d want min=1000 max=1000", seen[0].min, seen[0].max)
	}
	if seen[1].min != 2000 || seen[1].max != 2000 {
		t.Fatalf("second folder acquisition range mismatch: got min=%d max=%d want min=2000 max=2000", seen[1].min, seen[1].max)
	}
}

func TestSetProtectedPatternsMarksMatchingSearchResults(t *testing.T) {
	dbFile := filepath.Join(t.TempDir(), "db.sqlite")
	filler, err := datafillersqlite.InitDataFillerSqlite(dbFile)
	if err != nil {
		t.Fatalf("InitDataFillerSqlite returned error: %v", err)
	}
	if err := filler.SetSourceInfo("computername", "E:", 1000); err != nil {
		t.Fatalf("SetSourceInfo returned error: %v", err)
	}
	if err := filler.AddFile(dataprovider.FileInfo{Path: "folder/secret.txt", Size: 10, Mtime: 100, Atime: 200, Uid: 7}); err != nil {
		t.Fatalf("AddFile secret file returned error: %v", err)
	}
	if err := filler.AddFile(dataprovider.FileInfo{Path: "folder/public.txt", Size: 20, Mtime: 100, Atime: 200, Uid: 7}); err != nil {
		t.Fatalf("AddFile public file returned error: %v", err)
	}
	if err := filler.SetProtectedPatterns([]string{"secret"}); err != nil {
		t.Fatalf("SetProtectedPatterns returned error: %v", err)
	}
	filler.Finalize()

	p, err := dataprovidersqlite.InitDataProviderSqlite(dbFile)
	if err != nil {
		t.Fatalf("InitDataProviderSqlite returned error: %v", err)
	}
	defer p.Finalize()

	results, _, err := p.Search(context.Background(), dataprovider.SearchSelection{
		Path: "txt", ResultsLimit: 10, Kind: -1,
		SizeMin: -1, SizeMax: -1, MtimeMin: -1, MtimeMax: -1, AtimeMin: -1, AtimeMax: -1,
	})
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}
	protectedByPath := make(map[string]bool)
	for _, r := range results {
		protectedByPath[r.Path] = r.Extra["protected"] == true
	}
	if !protectedByPath["E:/folder/secret.txt"] {
		t.Fatalf("expected secret.txt to be marked protected, got results: %#v", results)
	}
	if protectedByPath["E:/folder/public.txt"] {
		t.Fatalf("expected public.txt to not be marked protected, got results: %#v", results)
	}
}

func TestSetProtectedPatternsMarksDirInfoAndSubdirs(t *testing.T) {
	dbFile := filepath.Join(t.TempDir(), "db.sqlite")
	filler, err := datafillersqlite.InitDataFillerSqlite(dbFile)
	if err != nil {
		t.Fatalf("InitDataFillerSqlite returned error: %v", err)
	}
	if err := filler.SetSourceInfo("computername", "E:", 1000); err != nil {
		t.Fatalf("SetSourceInfo returned error: %v", err)
	}
	if err := filler.AddFile(dataprovider.FileInfo{Path: "confidential/a.txt", Size: 10, Mtime: 100, Atime: 200, Uid: 7}); err != nil {
		t.Fatalf("AddFile returned error: %v", err)
	}
	if err := filler.AddFile(dataprovider.FileInfo{Path: "public/b.txt", Size: 20, Mtime: 100, Atime: 200, Uid: 7}); err != nil {
		t.Fatalf("AddFile returned error: %v", err)
	}
	if err := filler.RebuildDirTable(1000, nil); err != nil {
		t.Fatalf("RebuildDirTable returned error: %v", err)
	}
	if err := filler.SetProtectedPatterns([]string{"^confidential$"}); err != nil {
		t.Fatalf("SetProtectedPatterns returned error: %v", err)
	}
	filler.Finalize()

	p, err := dataprovidersqlite.InitDataProviderSqlite(dbFile)
	if err != nil {
		t.Fatalf("InitDataProviderSqlite returned error: %v", err)
	}
	defer p.Finalize()

	info, err := p.DirInfo("computername", "E:/confidential")
	if err != nil {
		t.Fatalf("DirInfo returned error: %v", err)
	}
	if got, _ := info["protected"].(bool); !got {
		t.Fatalf("expected DirInfo for 'confidential' to be marked protected, got: %#v", info["protected"])
	}

	rootInfo, err := p.DirInfo("computername", "E:")
	if err != nil {
		t.Fatalf("DirInfo returned error: %v", err)
	}
	subDirs, ok := rootInfo["subDirs"].([]dataprovider.SubDirStats)
	if !ok {
		t.Fatalf("subDirs has unexpected type: %#v", rootInfo["subDirs"])
	}
	protectedByName := make(map[string]bool)
	for _, sd := range subDirs {
		protectedByName[sd.Name] = sd.Extra["protected"] == true
	}
	if !protectedByName["confidential"] {
		t.Fatalf("expected subdir 'confidential' to be marked protected, got: %#v", subDirs)
	}
	if protectedByName["public"] {
		t.Fatalf("expected subdir 'public' to not be marked protected, got: %#v", subDirs)
	}
}
