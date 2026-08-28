package dataprovidersqlite

import (
	"database/sql"
	"path"
	"regexp"
	"strings"

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
	// Prepared statements
	stmtSelectPathElem *sql.Stmt
	stmtInsertPathElem *sql.Stmt
	stmtSelectPath     *sql.Stmt
	stmtInsertPath     *sql.Stmt
	stmtInsertFile     *sql.Stmt
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

	// Prepare statements for repeated use
	d.stmtSelectPathElem, err = db.Prepare(`SELECT id FROM path_elem WHERE elem = ?`)
	if err != nil {
		return nil, err
	}
	d.stmtInsertPathElem, err = db.Prepare(`INSERT INTO path_elem (elem) VALUES (?)`)
	if err != nil {
		return nil, err
	}
	d.stmtSelectPath, err = db.Prepare(`SELECT id FROM path WHERE path_elem_id = ? AND parent_id = ? AND is_dir = ?`)
	if err != nil {
		return nil, err
	}
	d.stmtInsertPath, err = db.Prepare(`INSERT INTO path (path_elem_id, parent_id, is_dir) VALUES (?, ?, ?)`)
	if err != nil {
		return nil, err
	}
	d.stmtInsertFile, err = db.Prepare(`INSERT INTO file2 (path_id, size, mtime, atime, uid) VALUES (?, ?, ?, ?, ?)`)
	if err != nil {
		return nil, err
	}

	return &d, nil
}

func (d *DataProviderSqlite) Finalize() {
	// Close prepared statements
	if d.stmtSelectPathElem != nil {
		d.stmtSelectPathElem.Close()
	}
	if d.stmtInsertPathElem != nil {
		d.stmtInsertPathElem.Close()
	}
	if d.stmtSelectPath != nil {
		d.stmtSelectPath.Close()
	}
	if d.stmtInsertPath != nil {
		d.stmtInsertPath.Close()
	}
	if d.stmtInsertFile != nil {
		d.stmtInsertFile.Close()
	}
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

// Database design
// The database stores for each file:
// - The file's path
// - The file's size
// - The file's modification time
// - The file's access time
// - The file's owner (uid)
// The database stores for each directory:
// - The directory's path
// - The total size of files in the directory
// - The acquisition time range of files in the directory
// - The distribution of file modification times and sizes in the directory
// - The distribution of file access times and sizes in the directory
// The database tries to minimize the amount of data needed,
// while still allowing fast lookups of files and directories.
// Since path elements are often repeated, they are stored in a separate table path_elem.
// The path table stores the hierarchy/tree of path elements.
// The file table stores the file information, linked to the path table.
// The dir table stores the directory information, linked to the path table.

func createTables(db *sql.DB) error {
	// Create tables if they don't exist
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS path_elem (
		id INTEGER PRIMARY KEY,
		elem TEXT UNIQUE
	)`)
	if err != nil {
		return err
	}

	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS path (
		id INTEGER PRIMARY KEY,
		parent_id INTEGER,
		path_elem_id INTEGER,
		is_dir INTEGER
		-- foreign keys disabled FOREIGN KEY(path_id) REFERENCES path(id)
	)`)
	if err != nil {
		return err
	}

	// Should we add is_dir to the index? There is usually only one item per path element
	_, err = db.Exec(`CREATE INDEX IF NOT EXISTS path_elem_par_idx ON path (path_elem_id, parent_id)`)
	if err != nil {
		return err
	}

	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS file2 (
		id INTEGER PRIMARY KEY,
		path_id INTEGER,
		size INTEGER,
		mtime INTEGER,
		atime INTEGER,
		uid INTEGER
		-- foreign keys disabled FOREIGN KEY(path_id) REFERENCES path(id)
	)`)
	if err != nil {
		return err
	}

	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS simple_path_elem (
		id INTEGER PRIMARY KEY,
		elem TEXT UNIQUE
	)`)
	if err != nil {
		return err
	}

	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS simple_path_dir (
		id INTEGER PRIMARY KEY,
		simple_path_elem_id INTEGER,
		dir_id INTEGER
	)`)
	if err != nil {
		return err
	}

	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS simple_path_file (
		id INTEGER PRIMARY KEY,
		simple_path_elem_id INTEGER,
		file_id INTEGER
	)`)
	if err != nil {
		return err
	}

	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS dir (
		id INTEGER PRIMARY KEY,
		path_id INTEGER NOT NULL DEFAULT 0,
		file_count INTEGER NOT NULL DEFAULT 0,
		total_size INTEGER NOT NULL DEFAULT 0,
		acqtime_min   INTEGER NOT NULL DEFAULT 0,
		acqtime_max   INTEGER NOT NULL DEFAULT 0,
		mtime_size_1m INTEGER NOT NULL DEFAULT 0,
		mtime_size_3m INTEGER NOT NULL DEFAULT 0,
		mtime_size_1y INTEGER NOT NULL DEFAULT 0,
		mtime_size_3y INTEGER NOT NULL DEFAULT 0,
		mtime_size_5y INTEGER NOT NULL DEFAULT 0,
		mtime_size_older INTEGER NOT NULL DEFAULT 0,
		atime_size_1m INTEGER NOT NULL DEFAULT 0,
		atime_size_3m INTEGER NOT NULL DEFAULT 0,
		atime_size_1y INTEGER NOT NULL DEFAULT 0,
		atime_size_3y INTEGER NOT NULL DEFAULT 0,
		atime_size_5y INTEGER NOT NULL DEFAULT 0,
		atime_size_older INTEGER NOT NULL DEFAULT 0
		-- foreign keys disabled FOREIGN KEY(path_id) REFERENCES path(id)
	)`)
	if err != nil {
		return err
	}
	// Add indexes for common query patterns
	_, err = db.Exec(`CREATE INDEX IF NOT EXISTS file_path_idx ON file2 (path_id)`)
	if err != nil {
		return err
	}
	_, err = db.Exec(`CREATE INDEX IF NOT EXISTS file_mtime_idx ON file2 (mtime)`)
	if err != nil {
		return err
	}
	_, err = db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS dir_path_unique_idx ON dir (path_id)`)
	if err != nil {
		return err
	}
	_, err = db.Exec(`CREATE INDEX IF NOT EXISTS dir_path_idx ON dir (path_id)`)
	if err != nil {
		return err
	}

	return nil
}

// Generate a simplified path element for searching/matching
func simplifyPathElem(elem string) string {
	// Remove extension
	p := strings.Index(elem, ".")
	if p != -1 {
		elem = elem[:p]
	}
	// remove leading and trailing whitespace
	elem = strings.TrimSpace(elem)
	// remove leading zeros
	elem = strings.TrimLeft(elem, "0")
	// remove all non-alphanumeric characters
	elem = regexp.MustCompile(`[^a-zA-Z0-9]`).ReplaceAllString(elem, "")
	// convert to lowercase
	elem = strings.ToLower(elem)
	return elem
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
	if len(elems) == 0 {
		return nil
	}

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
		err := d.db.QueryRow(`SELECT id FROM path WHERE path_elem_id = ? AND parent_id = ?`, peId, parentId).Scan(&id)
		if err != nil {
			// not found, nothing to delete
			return nil
		}
		parentId = id
	}

	// Delete all files and directories under this directory.
	tx, err := d.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	_, err = tx.Exec(`
		WITH RECURSIVE subtree AS (
			SELECT id FROM path WHERE id = ?
			UNION ALL
			SELECT p.id
			FROM path p
			INNER JOIN subtree s ON p.parent_id = s.id
		)
		DELETE FROM file2 WHERE path_id IN (SELECT id FROM subtree)
	`, parentId)
	if err != nil {
		return err
	}

	_, err = tx.Exec(`
		WITH RECURSIVE subtree AS (
			SELECT id FROM path WHERE id = ?
			UNION ALL
			SELECT p.id
			FROM path p
			INNER JOIN subtree s ON p.parent_id = s.id
		)
		DELETE FROM dir WHERE path_id IN (SELECT id FROM subtree)
	`, parentId)
	if err != nil {
		return err
	}

	_, err = tx.Exec(`
		WITH RECURSIVE subtree AS (
			SELECT id FROM path WHERE id = ?
			UNION ALL
			SELECT p.id
			FROM path p
			INNER JOIN subtree s ON p.parent_id = s.id
		)
		DELETE FROM path WHERE id IN (SELECT id FROM subtree)
	`, parentId)
	if err != nil {
		return err
	}

	return tx.Commit()
}

func (d *DataProviderSqlite) SourceInfo() (string, string, int64) {
	return d.computerName, d.basePath, d.acqTime
}

func (d *DataProviderSqlite) resolvePathID(dir string) (int64, error) {
	trimmed := strings.TrimSpace(dir)
	if trimmed == "" || trimmed == "." {
		trimmed = path.Join(d.computerName, d.basePath)
	} else {
		trimmed = path.Join(d.computerName, d.basePath, trimmed)
	}
	trimmed = path.Clean(trimmed)
	if trimmed == "." {
		trimmed = path.Join(d.computerName, d.basePath)
	}

	elems := splitPath(trimmed)
	if len(elems) == 0 {
		return 0, sql.ErrNoRows
	}

	parentID := int64(-1)
	for _, elem := range elems {
		var pathElemID int64
		if err := d.db.QueryRow(`SELECT id FROM path_elem WHERE elem = ?`, elem).Scan(&pathElemID); err != nil {
			return 0, err
		}
		var id int64
		if err := d.db.QueryRow(`SELECT id FROM path WHERE path_elem_id = ? AND parent_id = ? AND is_dir = 1`, pathElemID, parentID).Scan(&id); err != nil {
			return 0, err
		}
		parentID = id
	}
	return parentID, nil
}

func (d *DataProviderSqlite) DirExists(dir string) (bool, error) {
	_, err := d.resolvePathID(dir)
	if err == nil {
		return true, nil
	}
	if err == sql.ErrNoRows {
		return false, nil
	}
	return false, err
}

func (d *DataProviderSqlite) DirSizeModTimeBin(dir string, bin int) (uint64, error) {
	pathID, err := d.resolvePathID(dir)
	if err != nil {
		return 0, err
	}
	var value int64
	col := "mtime_size_1m"
	switch bin {
	case 0:
		col = "mtime_size_1m"
	case 1:
		col = "mtime_size_3m"
	case 2:
		col = "mtime_size_1y"
	case 3:
		col = "mtime_size_3y"
	case 4:
		col = "mtime_size_5y"
	default:
		col = "mtime_size_older"
	}
	query := `SELECT ` + col + ` FROM dir WHERE path_id = ?`
	if err := d.db.QueryRow(query, pathID).Scan(&value); err != nil {
		if err == sql.ErrNoRows {
			return 0, nil
		}
		return 0, err
	}
	return uint64(value), nil
}

func (d *DataProviderSqlite) DirSizeAccTimeBin(dir string, bin int) (uint64, error) {
	pathID, err := d.resolvePathID(dir)
	if err != nil {
		return 0, err
	}
	var value int64
	col := "atime_size_1m"
	switch bin {
	case 0:
		col = "atime_size_1m"
	case 1:
		col = "atime_size_3m"
	case 2:
		col = "atime_size_1y"
	case 3:
		col = "atime_size_3y"
	case 4:
		col = "atime_size_5y"
	default:
		col = "atime_size_older"
	}
	query := `SELECT ` + col + ` FROM dir WHERE path_id = ?`
	if err := d.db.QueryRow(query, pathID).Scan(&value); err != nil {
		if err == sql.ErrNoRows {
			return 0, nil
		}
		return 0, err
	}
	return uint64(value), nil
}

func (d *DataProviderSqlite) SubDirs(dir string) ([]string, error) {
	pathID, err := d.resolvePathID(dir)
	if err != nil {
		return nil, err
	}
	rows, err := d.db.Query(`SELECT pe.elem FROM path p JOIN path_elem pe ON pe.id = p.path_elem_id WHERE p.parent_id = ? AND p.is_dir = 1 ORDER BY pe.elem`, pathID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]string, 0)
	for rows.Next() {
		var elem string
		if err := rows.Scan(&elem); err != nil {
			return nil, err
		}
		out = append(out, elem)
	}
	return out, rows.Err()
}

func (d *DataProviderSqlite) SubDirSize(dir string) (uint64, error) {
	pathID, err := d.resolvePathID(dir)
	if err != nil {
		return 0, err
	}
	var totalSize int64
	if err := d.db.QueryRow(`SELECT total_size FROM dir WHERE path_id = ?`, pathID).Scan(&totalSize); err != nil {
		if err == sql.ErrNoRows {
			return 0, nil
		}
		return 0, err
	}
	return uint64(totalSize), nil
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

	elemsIds := make([]int64, 0, len(elems))
	for _, e := range elems {
		var id int64
		err := d.stmtSelectPathElem.QueryRow(e).Scan(&id)
		if err != nil {
			// Add path element to table
			res, err := d.stmtInsertPathElem.Exec(e)
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

func (d *DataProviderSqlite) addFileDb2(f dataprovider.FileInfo, pathId int64) (int64, error) {
	// Add file to file table
	// Return id of file
	var id int64

	res, err := d.stmtInsertFile.Exec(pathId, f.Size, f.Mtime, f.Atime, f.Uid)
	if err != nil {
		return 0, err
	}
	id, err = res.LastInsertId()
	if err != nil {
		return 0, err
	}
	return id, err
}

func (d *DataProviderSqlite) ensurePath(peId int64, parentId int64, isDir int) (int64, error) {
	// Add path table
	// Return id of directory
	// FIXME: handle situation where directory name was previously a filename or vice versa
	// TODO: optimize, skip if same dir as previous

	var id int64
	err := d.stmtSelectPath.QueryRow(peId, parentId, isDir).Scan(&id)
	if err != nil {
		// Add path to table
		res, err := d.stmtInsertPath.Exec(peId, parentId, isDir)
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

type dirSummary struct {
	fileCount  int64
	totalSize  int64
	mtimeSize  [6]int64
	atimeSize  [6]int64
	acqtimeMin int64
	acqtimeMax int64
}

type dirTimeBin struct {
	maxAgeS int64
}

var dirTimeBins = [...]dirTimeBin{
	{maxAgeS: 3600 * 24 * 30},
	{maxAgeS: 3600 * 24 * 90},
	{maxAgeS: 3600 * 24 * 365},
	{maxAgeS: 3600 * 24 * 365 * 3},
	{maxAgeS: 3600 * 24 * 365 * 5},
	{maxAgeS: 3600 * 24 * 30 * 999},
}

func dirTimeBucket(t int64, acqTime int64) int {
	age := acqTime - t
	for i, tb := range dirTimeBins {
		if age < tb.maxAgeS {
			return i
		}
	}
	return len(dirTimeBins) - 1
}

func (d *DataProviderSqlite) ancestorDirIDs(pathID int64) ([]int64, error) {
	ids := make([]int64, 0, 8)
	for current := pathID; current > 0; {
		var parentID, isDir int64
		err := d.db.QueryRow(`SELECT parent_id, is_dir FROM path WHERE id = ?`, current).Scan(&parentID, &isDir)
		if err != nil {
			return nil, err
		}
		if isDir == 1 {
			ids = append(ids, current)
		}
		if parentID <= 0 {
			break
		}
		current = parentID
	}
	return ids, nil
}

func (d *DataProviderSqlite) flushDirSummaryBatch(stats map[int64]*dirSummary) error {
	if len(stats) == 0 {
		return nil
	}

	stmt, err := d.db.Prepare(`
		INSERT INTO dir (
			path_id, file_count, total_size, acqtime_min, acqtime_max,
			mtime_size_1m, mtime_size_3m, mtime_size_1y, mtime_size_3y, mtime_size_5y, mtime_size_older,
			atime_size_1m, atime_size_3m, atime_size_1y, atime_size_3y, atime_size_5y, atime_size_older
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(path_id) DO UPDATE SET
			file_count = dir.file_count + excluded.file_count,
			total_size = dir.total_size + excluded.total_size,
			acqtime_min = MIN(dir.acqtime_min, excluded.acqtime_min),
			acqtime_max = MAX(dir.acqtime_max, excluded.acqtime_max),
			mtime_size_1m = dir.mtime_size_1m + excluded.mtime_size_1m,
			mtime_size_3m = dir.mtime_size_3m + excluded.mtime_size_3m,
			mtime_size_1y = dir.mtime_size_1y + excluded.mtime_size_1y,
			mtime_size_3y = dir.mtime_size_3y + excluded.mtime_size_3y,
			mtime_size_5y = dir.mtime_size_5y + excluded.mtime_size_5y,
			mtime_size_older = dir.mtime_size_older + excluded.mtime_size_older,
			atime_size_1m = dir.atime_size_1m + excluded.atime_size_1m,
			atime_size_3m = dir.atime_size_3m + excluded.atime_size_3m,
			atime_size_1y = dir.atime_size_1y + excluded.atime_size_1y,
			atime_size_3y = dir.atime_size_3y + excluded.atime_size_3y,
			atime_size_5y = dir.atime_size_5y + excluded.atime_size_5y,
			atime_size_older = dir.atime_size_older + excluded.atime_size_older
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for pathID, summary := range stats {
		_, err = stmt.Exec(
			pathID,
			summary.fileCount,
			summary.totalSize,
			summary.acqtimeMin,
			summary.acqtimeMax,
			summary.mtimeSize[0],
			summary.mtimeSize[1],
			summary.mtimeSize[2],
			summary.mtimeSize[3],
			summary.mtimeSize[4],
			summary.mtimeSize[5],
			summary.atimeSize[0],
			summary.atimeSize[1],
			summary.atimeSize[2],
			summary.atimeSize[3],
			summary.atimeSize[4],
			summary.atimeSize[5],
		)
		if err != nil {
			return err
		}
	}
	return nil
}

func (d *DataProviderSqlite) RebuildDirTable(batchSize int, progress dataprovider.ProgressFunc) error {
	if batchSize <= 0 {
		batchSize = 1000
	}

	var totalRows int64
	if err := d.db.QueryRow(`SELECT COUNT(*) FROM file2`).Scan(&totalRows); err != nil {
		return err
	}

	if _, err := d.db.Exec(`DELETE FROM dir`); err != nil {
		return err
	}

	stats := make(map[int64]*dirSummary)
	processedTotal := int64(0)
	for offset := 0; ; offset += batchSize {
		rows, err := d.db.Query(`SELECT path_id, size, mtime, atime FROM file2 ORDER BY id LIMIT ? OFFSET ?`, batchSize, offset)
		if err != nil {
			return err
		}

		processed := false
		for rows.Next() {
			processed = true
			processedTotal++
			var pathID int64
			var size int64
			var mtime int64
			var atime int64
			if err := rows.Scan(&pathID, &size, &mtime, &atime); err != nil {
				rows.Close()
				return err
			}

			dirs, err := d.ancestorDirIDs(pathID)
			if err != nil {
				rows.Close()
				return err
			}
			for _, dirID := range dirs {
				sum := stats[dirID]
				if sum == nil {
					sum = &dirSummary{acqtimeMin: d.acqTime, acqtimeMax: d.acqTime}
					stats[dirID] = sum
				}
				sum.fileCount++
				sum.totalSize += size
				mtimeBucket := dirTimeBucket(mtime, d.acqTime)
				sum.mtimeSize[mtimeBucket] += size
				atimeBucket := dirTimeBucket(atime, d.acqTime)
				sum.atimeSize[atimeBucket] += size
				if sum.acqtimeMin == 0 || d.acqTime < sum.acqtimeMin {
					sum.acqtimeMin = d.acqTime
				}
				if d.acqTime > sum.acqtimeMax {
					sum.acqtimeMax = d.acqTime
				}
			}
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return err
		}
		rows.Close()
		if !processed {
			break
		}
		if progress != nil {
			progress(processedTotal, totalRows)
		}
		if err := d.flushDirSummaryBatch(stats); err != nil {
			return err
		}
		stats = make(map[int64]*dirSummary)
	}

	return nil
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
	// add path elements leading up to this file to path table
	pathId := int64(-1)
	for _, peId := range elemsIds[:len(elemsIds)-1] {
		pathId, err = d.ensurePath(peId, pathId, 1)
		if err != nil {
			return err
		}
	}
	// add file to path table (with is_dir = 0)
	pathId, err = d.ensurePath(elemsIds[len(elemsIds)-1], pathId, 0)
	if err != nil {
		return err
	}
	// Add file info to file table
	_, err = d.addFileDb2(f, pathId)

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

type SameFiles struct {
	Files []dataprovider.FileInfo
}

func (d *DataProviderSqlite) FindSameFiles(minSize uint64, minTimeDiff int64, maxTimeDiff int64) ([]SameFiles, error) {
	// Find files with same size and same mtime
	// Return slice of SameFiles

	return nil, nil
}
