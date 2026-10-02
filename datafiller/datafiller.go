package datafiller

import "github.com/524D/filelist2db/dataprovider"

// DataFiller is the write-side interface for importing files into a database
// and rebuilding its summary tables.
type DataFiller interface {
	// SetSourceInfo registers the data source name, base path, and acquisition
	// time for subsequent AddFile calls, removing any previously stored data
	// for the same source/base path.
	SetSourceInfo(dataSource string, basePath string, acqTime int64) error
	// AddFile inserts a single file's metadata into the database.
	AddFile(dataprovider.FileInfo) error
	// RebuildDirTable recomputes the per-directory size/file-count/time-bucket
	// summary table, reporting progress via progress if non-nil.
	RebuildDirTable(batchSize int, progress dataprovider.ProgressFunc) error
	// RebuildBinTable recomputes the file-size histogram table, reporting
	// progress via progress if non-nil.
	RebuildBinTable(batchSize int, progress dataprovider.ProgressFunc) error
	// SetProtectedPatterns replaces the full set of regular expressions used to
	// mark matching file/directory names as protected.
	SetProtectedPatterns(patterns []string) error
	// Finalize closes all resources associated with the data filler.
	Finalize()
	// StartTransaction begins a database transaction.
	StartTransaction() error
	// CommitTransaction commits the transaction started by StartTransaction.
	CommitTransaction() error
}
