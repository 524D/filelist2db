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
	SourceInfo() (string, string, int64)
	DataSources() ([]string, error)
	DirExists(source string, dir string) (bool, error)
	DirSizeTimeBins(source string, dir string) ([]uint64, []uint64, []TimeBin, error)
	SubDirs(source string, dir string) ([]string, error)
	SubDirSize(source string, dir string) (uint64, error)
	Search(selection SearchSelection) ([]SearchResult, map[string]interface{}, error)
	SearchByName(name string, limit int) ([]SearchResult, map[string]interface{}, error)
	SearchBySimpleName(name string, limit int) ([]SearchResult, map[string]interface{}, error)
	Finalize()
}
