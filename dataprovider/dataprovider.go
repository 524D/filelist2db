package dataprovider

type UidT uint64

type ProgressFunc func(current int64, total int64)
type FileInfo struct {
	Path       string
	Size       uint64
	Uid        UidT
	Mtime      int64
	Atime      int64
	AtimeValid bool
}

type SearchResult struct {
	Kind      string
	Path      string
	Size      uint64
	Mtime     int64
	Atime     int64
	FileCount int64
	TotalSize uint64
}

// SubDirStats contains info about a subdirectory: its name, cumulative size, and cumulative file count.
type SubDirStats struct {
	Name      string
	Size      uint64
	FileCount int64
}

type TimeBin struct {
	MaxAgeS uint64 // Maximum age in seconds
	Txt     string // Textual description of time bin
}

type SearchSelection struct {
	Kind         int64
	Path         string
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

type DataProvider interface {
	// Returns the list of data sources available in the provider.
	// Sources are either a computer name or a network share name
	DataSources() ([]string, error)
	// Returns a map, where the keys are the names of the directories in the given source and path,
	// and the values are the corresponding directory information.
	DirInfo(source string, dir string) (map[string]any, error)
	// Returns the list of files and directories that match the given search criteria.
	Search(selection SearchSelection) ([]SearchResult, map[string]interface{}, error)
	// Returns the list of files and directories that match the given simplified name.
	SearchBySimpleName(name string, limit int) ([]SearchResult, map[string]interface{}, error)
	// Close all resources associated with the data provider.
	Finalize()
}
