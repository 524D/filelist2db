package dataprovidersqlite

import (
	"database/sql"
	"path"

	"github.com/524D/filelist2db/dataprovider"
	_ "modernc.org/sqlite"
)

// DataProviderSqlite implements the DataProvider interface
type DataProviderSqlite struct {
	db              *sql.DB
	computerName    string
	basePath        string
	acqTime         int64
	prevDirElemsIds []int64 // Path elements IDs of previous file

}

func InitDataProviderSqlite(dbFile string) (dataprovider.DataProvider, error) {
	// Open database
	db, err := openDatabase(dbFile)
	if err != nil {
		return nil, err
	}

	// Create tables if they don't exist
	err = createTables(db)
	if err != nil {
		return nil, err
	}

	d := DataProviderSqlite{db: db}

	// Don't wait for data to be written to disk
	_, err = db.Exec(`PRAGMA synchronous = OFF`)
	if err != nil {
		return nil, err
	}

	return &d, nil
}

func (d *DataProviderSqlite) Finalize() {
	d.db.Close()
}

func openDatabase(dbFile string) (*sql.DB, error) {
	// Open sqlite database
	// If database doesn't exist, create it

	db, err := sql.Open("sqlite", dbFile)
	if err != nil {
		return nil, err
	}
	return db, nil
}

func createTables(db *sql.DB) error {
	// Create tables if they don't exist
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS path_elem (
		id INTEGER PRIMARY KEY,
		elem TEXT UNIQUE
	)`)
	if err != nil {
		return err
	}

	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS dir (
		id INTEGER PRIMARY KEY,
		parent_id INTEGER,
		path_elem_id INTEGER,
		tot_size INTEGER,
		acqtime_min   INTEGER,
		acqtime_max   INTEGER,
		mtime_size_1m INTEGER,
		mtime_size_3m INTEGER,
		mtime_size_1y INTEGER,
		mtime_size_3y INTEGER,
		mtime_size_5y INTEGER,
		mtime_size_older INTEGER,
		atime_size_1m INTEGER,
		atime_size_3m INTEGER,
		atime_size_1y INTEGER,
		atime_size_3y INTEGER,
		atime_size_5y INTEGER,
		atime_size_older INTEGER
		-- foreign keys disabled FOREIGN KEY(parent_id) REFERENCES dir(id)
	)`)
	if err != nil {
		return err
	}
	_, err = db.Exec(`CREATE INDEX IF NOT EXISTS dir_elem_par_idx ON dir (path_elem_id, parent_id)`)
	if err != nil {
		return err
	}

	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS file (
		id INTEGER PRIMARY KEY,
		path_elem_id INTEGER,
		parent_id INTEGER,
		size INTEGER,
		mtime INTEGER,
		atime INTEGER,
		uid INTEGER
		-- foreign keys disabled FOREIGN KEY(parent_id) REFERENCES dir(id)
	)`)
	if err != nil {
		return err
	}

	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS file (
		id INTEGER PRIMARY KEY,
		acqtime INTEGER,
		file_id INTEGER
		-- foreign keys disabled FOREIGN KEY(file_id) REFERENCES file(id)
	)`)
	if err != nil {
		return err
	}

	return nil
}

// Get the numerical ids for the file path elements in elems
func (d *DataProviderSqlite) getElemsIds(elems []string) ([]int64, error) {
	elemsIds := make([]int64, 0, len(elems))
	for _, e := range elems {
		var id int64
		err := d.db.QueryRow(`SELECT id FROM path_elem WHERE elem = ?`, e).Scan(&id)
		if err != nil {
			return nil, err
		}
		elemsIds = append(elemsIds, id)
	}

	return elemsIds, nil
}

func (d *DataProviderSqlite) SetSourceInfo(computerName string, basePath string, acqTime int64) error {
	d.computerName = computerName
	d.basePath = basePath
	d.acqTime = acqTime
	elems := splitPath(path.Join(computerName, basePath))
	// Remove all data from previous run for this source
	elemsIds, err := d.getElemsIds(elems)
	if err != nil {
		// not found, nothing to delete
		return nil
	}
	// find the top level directory for this source
	parentId := int64(-1)
	for _, peId := range elemsIds {
		var id int64
		err := d.db.QueryRow(`SELECT id FROM dir WHERE path_elem_id = ? AND parent_id = ?`, peId, parentId).Scan(&id)
		if err != nil {
			// not found, nothing to delete
			return nil
		}
		parentId = id
	}

	// FIXME: multiple problems here:
	// - we don't update the top level dir cumulative sizes
	// - recursion does not work like this

	// Delete all files and directories under this directory
	for {
		// Delete files
		_, err = d.db.Exec(`DELETE FROM file WHERE parent_id = ?`, parentId)
		if err != nil {
			return err
		}
		var id int64
		err := d.db.QueryRow(`SELECT id FROM dir WHERE parent_id = ?`, parentId).Scan(&id)
		if err != nil {
			return nil
		}
		parentId = id
	}
}

func (d *DataProviderSqlite) SourceInfo() (string, string, int64) {
	return d.computerName, d.basePath, d.acqTime
}

func splitPath(fn string) []string {
	// split path in elements
	elems := make([]string, 0, 10)
	d := fn
	for ; d != "/" && d != "."; d = path.Dir(d) {
		elems = append(elems, path.Base(d))
	}
	// Reverse slice
	// FIXME: use slices.Reverse(s) when Go 1.21 is released
	for i, j := 0, len(elems)-1; i < j; i, j = i+1, j-1 {
		elems[i], elems[j] = elems[j], elems[i]
	}
	return elems
}

func (d *DataProviderSqlite) addPathElems(elems []string) ([]int64, error) {
	// Add path elements to path_elem table
	// Return slice with ids of path elements
	// If path element already exists, don't add it again

	// FIXME: use prepared statements
	elemsIds := make([]int64, 0, len(elems))
	for _, e := range elems {
		var id int64
		err := d.db.QueryRow(`SELECT id FROM path_elem WHERE elem = ?`, e).Scan(&id)
		if err != nil {
			// Add path element to table
			res, err := d.db.Exec(`INSERT INTO path_elem (elem) VALUES (?)`, e)
			if err != nil {
				return nil, err
			}
			id, err = res.LastInsertId()
			if err != nil {
				return nil, err
			}
		}
		elemsIds = append(elemsIds, id)
	}

	return elemsIds, nil
}

func (d *DataProviderSqlite) addFileDb(f dataprovider.FileInfo, pathElemId int64, parentId int64) (int64, error) {
	// Add file to file table
	// Return id of file
	var id int64

	res, err := d.db.Exec(`INSERT INTO file (path_elem_id, parent_id, size, mtime, atime, uid) VALUES (?, ?, ?, ?, ?, ?)`,
		pathElemId, parentId, f.Size, f.Mtime, f.Atime, f.Uid)
	if err != nil {
		return 0, err
	}
	id, err = res.LastInsertId()
	if err != nil {
		return 0, err
	}
	return id, err
}

func (d *DataProviderSqlite) ensureDir(peId int64, parentId int64, depth int) (int64, error) {
	// Add directory to dir table
	// Return id of directory
	// TODO: optimize, skip if same dir as previous

	var id int64
	err := d.db.QueryRow(`SELECT id FROM dir WHERE path_elem_id = ? AND parent_id = ?`, peId, parentId).Scan(&id)
	if err != nil {
		// Add directory to table
		res, err := d.db.Exec(`INSERT INTO dir (path_elem_id, parent_id) VALUES (?, ?)`, peId, parentId)
		if err != nil {
			return 0, err
		}
		id, err = res.LastInsertId()
		if err != nil {
			return 0, err
		}
	}
	return id, nil
}

func (d *DataProviderSqlite) AddFile(f dataprovider.FileInfo) error {
	// Add computername and leading dir to path
	fullPath := path.Join(d.computerName, d.basePath, f.Path)

	// split path in elements
	elems := splitPath(fullPath)

	// add elements to path_elem table
	elemsIds, err := d.addPathElems(elems)
	if err != nil {
		return err
	}
	// add directories leading up to this file to dir table
	dirId := int64(-1)
	for i, peId := range elemsIds[:len(elemsIds)-1] {
		dirId, err = d.ensureDir(peId, dirId, i)
		if err != nil {
			return err
		}
	}

	// add file to file table
	_, err = d.addFileDb(f, elemsIds[len(elemsIds)-1], dirId)
	if err != nil {
		return err
	}

	// We assume that files are added sorted by path
	// To avoid having to update the dir table for every file, we only update it
	// when the a new path element is encountered

	// Store the path elements IDs if we don't have them yet
	if d.prevDirElemsIds == nil {
		d.prevDirElemsIds = elemsIds[:len(elemsIds)-1]
	} else {
		// Skip over common prefix of previous and current path elements
		i := 0
		for ; i < len(d.prevDirElemsIds) && i < len(elemsIds) && d.prevDirElemsIds[i] == elemsIds[i]; i++ {
		}
		// If file is in a new directory, write dir table for paths of higher dir levels
	}

	// FIXME: write dir info after the very last file

	// accTimeBin := binTime(f.atime, d.acqTime)
	// modTimeBin := binTime(f.mtime, d.acqTime)
	// lastDir := ``
	// for dir := path.Dir(f.path); lastDir != `.` && lastDir != `/`; dir = path.Dir(dir) {
	// 	var ds dirSummary
	// 	var ok bool
	// 	// Create dir summary item if this is the first time we encountered this dir
	// 	if ds, ok = d.dirSummap[dir]; !ok {
	// 		ds.dirs = make(map[string]void)
	// 		ds.uids = make(map[uidT]void)
	// 	}

	// 	if lastDir != `` {
	// 		ds.dirs[lastDir] = member
	// 	}
	// 	ds.sizeAccTm[accTimeBin] += f.size
	// 	ds.sizeModTm[modTimeBin] += f.size
	// 	ds.uids[uidT(f.uid)] = member
	// 	d.dirSummap[dir] = ds
	// 	lastDir = path.Base(dir)
	// }

	return nil
}

func (d *DataProviderSqlite) StartTransaction() error {
	_, err := d.db.Exec(`BEGIN TRANSACTION`)
	return err
}

func (d *DataProviderSqlite) CommitTransaction() error {
	_, err := d.db.Exec(`END TRANSACTION`)
	return err
}
