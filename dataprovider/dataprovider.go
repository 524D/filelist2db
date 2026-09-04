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

type DataProvider interface {
	SetSourceInfo(computerName string, basePath string, acqTime int64) error
	SourceInfo() (string, string, int64)
	DataSources() ([]string, error)
	AddFile(FileInfo) error
	RebuildDirTable(batchSize int, progress ProgressFunc) error
	DirExists(dir string) (bool, error)
	DirSizeModTimeBin(dir string, bin int) (uint64, error)
	DirSizeAccTimeBin(dir string, bin int) (uint64, error)
	SubDirs(dir string) ([]string, error)
	SubDirSize(dir string) (uint64, error)
	Finalize()
	StartTransaction() error
	CommitTransaction() error
}
