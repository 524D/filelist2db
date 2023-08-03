package dataprovider

type UidT uint64

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
	AddFile(FileInfo) error
	Finalize()
	StartTransaction() error
	CommitTransaction() error
}
