package dataprovider

import "context"

// UidT is a file owner's numeric user ID (uid).
type UidT uint64

// ProgressFunc reports batch-processing progress, where current/total are
// the number of items processed so far out of the total to process.
type ProgressFunc func(current int64, total int64)

// FileInfo describes a single file's metadata, as read from a file-list input.
type FileInfo struct {
	Path       string
	Size       uint64
	Uid        UidT
	Mtime      int64
	Atime      int64
	AtimeValid bool
}

// SearchResult is a single file or directory match returned by Search or SearchBySimpleName.
type SearchResult struct {
	Kind      string // "file" or "dir"
	Source    string // Data source name, e.g. computer name or network share name
	Path      string // Path, if data source is a computer name the computer name is not included in the path, if data source is a network share name the share name IS included in the path
	Size      uint64 // For files, the file size in bytes. No used for directories.
	Mtime     int64  // For files, the last modification time as a Unix timestamp. Not used for directories.
	Atime     int64  // For files, the last access time as a Unix timestamp. Not used for directories.
	FileCount int64  // For directories, the number of files in the directory. Not used for files.
	TotalSize uint64 // For directories, the total size of all files in the directory. Not used for files.
	// Extra holds extensible, ad-hoc result attributes, e.g. Extra["protected"] (bool).
	Extra map[string]any
}

// SubDirStats contains info about a subdirectory: its name, cumulative size, and cumulative file count.
type SubDirStats struct {
	Name      string
	Size      uint64
	FileCount int64
	// Extra holds extensible, ad-hoc attributes, e.g. Extra["protected"] (bool).
	Extra map[string]any
}

type TimeBin struct {
	MaxAgeS uint64 // Maximum age in seconds
	Txt     string // Textual description of time bin
}

// SameFiles groups files considered duplicates by FindSameFiles.
type SameFiles struct {
	Source string
	Size   uint64
	Files  []FileInfo
}

// SearchSelection holds the filter criteria and pagination parameters used by Search.
type SearchSelection struct {
	Kind         int64
	Path         string
	Source       string
	SimplePath   bool
	SizeMin      int64
	SizeMax      int64
	MtimeMin     int64
	MtimeMax     int64
	AtimeMin     int64
	AtimeMax     int64
	ResultsFirst int64
	ResultsLimit int64
}

// DataProvider is the read-only interface for querying an imported file database.
type DataProvider interface {
	// DataSources returns the list of data sources available in the provider.
	// Sources are either a computer name or a network share name.
	DataSources() ([]string, error)
	// DirInfo returns a map describing the directory at source+dir, or nil if it
	// doesn't exist. Keys include "mtimes"/"atimes" (size-by-age []uint64 buckets,
	// see TimeBin), "acqTimeMin"/"acqTimeMax" (int64 Unix timestamps), "genTime"
	// (int64 Unix timestamp of the last summary rebuild), "timeBins" ([]TimeBin
	// bucket descriptions), "subDirs" ([]SubDirStats), "protected" (bool), and
	// optionally "sizeBins" (per-size-bucket totals, only for the outermost
	// directory levels).
	DirInfo(source string, dir string) (map[string]any, error)
	// Search returns the files/directories matching selection, a metadata map
	// (e.g. "SearchTimeMicroSeconds"), and an error, if any. It honors
	// context cancellation.
	Search(ctx context.Context, selection SearchSelection) ([]SearchResult, map[string]interface{}, error)
	// SearchBySimpleName returns files/directories whose simplified name matches
	// name (up to limit results), while honoring ctx cancellation.
	SearchBySimpleName(ctx context.Context, name string, limit int) ([]SearchResult, map[string]interface{}, error)
	// FindSameFiles returns groups of files that have the same size and share the
	// same source/root, with files larger than minSize included.
	FindSameFiles(minSize uint64, minTimeDiff int64, maxTimeDiff int64) ([]SameFiles, error)
	// Finalize closes all resources associated with the data provider.
	Finalize()
}
