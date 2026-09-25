package datafiller

import "github.com/524D/filelist2db/dataprovider"

type DataFiller interface {
	SetSourceInfo(dataSource string, basePath string, acqTime int64) error
	AddFile(dataprovider.FileInfo) error
	RebuildDirTable(batchSize int, progress dataprovider.ProgressFunc) error
	RebuildBinTable(batchSize int, progress dataprovider.ProgressFunc) error
	Finalize()
	StartTransaction() error
	CommitTransaction() error
}
