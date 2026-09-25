package datafillersqlite

import (
	"database/sql"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/524D/filelist2db/datafiller"
	"github.com/524D/filelist2db/dataprovider"
	_ "modernc.org/sqlite"
)

const (
	nodeTypeFile         = 0
	nodeTypeDir          = 1
	nodeTypeComputerName = 2
	nodeTypeShareName    = 3
)

type DataFillerSqlite struct {
	db                            *sql.DB
	dataSource                    string
	basePath                      string
	acqTime                       int64
	prevDirElemsIds               []int64
	prevDir                       string
	prevPathId                    int64
	ancestorDirIDsCache           []int64
	ancestorDirIDCachePos         map[int64]int
	prevAncestorDirs              []int64
	stmtSelectPathElem            *sql.Stmt
	stmtInsertPathElem            *sql.Stmt
	stmtSelectPath                *sql.Stmt
	stmtInsertPath                *sql.Stmt
	stmtInsertFile                *sql.Stmt
	stmtInsertInputFile           *sql.Stmt
	stmtSelectSimplePathElem      *sql.Stmt
	stmtInsertSimplePathElem      *sql.Stmt
	stmtCountSimplePathTranslate  *sql.Stmt
	stmtInsertSimplePathTranslate *sql.Stmt
	stmtSelectPathParentInfo      *sql.Stmt
	stmtCountFiles                *sql.Stmt
	stmtDeleteDir                 *sql.Stmt
	stmtSelectFileBatch           *sql.Stmt
	stmtBeginTransaction          *sql.Stmt
	stmtCommitTransaction         *sql.Stmt
}

var _ datafiller.DataFiller = (*DataFillerSqlite)(nil)

func InitDataFillerSqlite(dbFile string) (*DataFillerSqlite, error) {
	db, err := openDatabase(dbFile)
	if err != nil {
		return nil, err
	}
	if err := createTables(db); err != nil {
		return nil, err
	}
	_, err = db.Exec(`PRAGMA synchronous = OFF`)
	if err != nil {
		return nil, err
	}

	d := &DataFillerSqlite{db: db}
	if d.stmtSelectPathElem, err = db.Prepare(`SELECT id FROM path_elem WHERE elem = ?`); err != nil {
		return nil, err
	}
	if d.stmtInsertPathElem, err = db.Prepare(`INSERT INTO path_elem (elem) VALUES (?)`); err != nil {
		return nil, err
	}
	if d.stmtSelectPath, err = db.Prepare(`SELECT id FROM path WHERE path_elem_id = ? AND parent_id = ? AND node_type = ?`); err != nil {
		return nil, err
	}
	if d.stmtInsertPath, err = db.Prepare(`INSERT INTO path (path_elem_id, parent_id, node_type) VALUES (?, ?, ?)`); err != nil {
		return nil, err
	}
	if d.stmtInsertFile, err = db.Prepare(`INSERT INTO file (path_id, size, mtime, atime, uid, acqtime) VALUES (?, ?, ?, ?, ?, ?)`); err != nil {
		return nil, err
	}
	if d.stmtInsertInputFile, err = db.Prepare(`INSERT INTO input_file (path_id, timestamp) VALUES (?, ?)`); err != nil {
		return nil, err
	}
	if d.stmtSelectSimplePathElem, err = db.Prepare(`SELECT id FROM simple_path_elem WHERE simple_elem = ?`); err != nil {
		return nil, err
	}
	if d.stmtInsertSimplePathElem, err = db.Prepare(`INSERT INTO simple_path_elem (simple_elem) VALUES (?)`); err != nil {
		return nil, err
	}
	if d.stmtCountSimplePathTranslate, err = db.Prepare(`SELECT COUNT(*) FROM simple_path_translate WHERE simple_path_elem_id = ? AND path_elem_id = ?`); err != nil {
		return nil, err
	}
	if d.stmtInsertSimplePathTranslate, err = db.Prepare(`INSERT INTO simple_path_translate (simple_path_elem_id, path_elem_id) VALUES (?, ?)`); err != nil {
		return nil, err
	}
	if d.stmtSelectPathParentInfo, err = db.Prepare(`SELECT parent_id, node_type FROM path WHERE id = ?`); err != nil {
		return nil, err
	}
	if d.stmtCountFiles, err = db.Prepare(`SELECT COUNT(*) FROM file`); err != nil {
		return nil, err
	}
	if d.stmtDeleteDir, err = db.Prepare(`DELETE FROM dir`); err != nil {
		return nil, err
	}
	if d.stmtSelectFileBatch, err = db.Prepare(`SELECT f.path_id, p.parent_id, f.size, f.mtime, f.atime, f.acqtime
		FROM file f
		JOIN path p ON p.id = f.path_id
		ORDER BY f.id LIMIT ? OFFSET ?`); err != nil {
		return nil, err
	}
	if d.stmtBeginTransaction, err = db.Prepare(`BEGIN TRANSACTION`); err != nil {
		return nil, err
	}
	if d.stmtCommitTransaction, err = db.Prepare(`END TRANSACTION`); err != nil {
		return nil, err
	}
	return d, nil
}

func openDatabase(dbFile string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(dbFile))
	if err != nil {
		return nil, err
	}
	if _, err = db.Exec(`PRAGMA cache_size = -131072`); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

func createTables(db *sql.DB) error {
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS path_elem (
		id INTEGER PRIMARY KEY,
		elem TEXT UNIQUE NOT NULL
	)`)
	if err != nil {
		return err
	}

	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS path (
		id INTEGER PRIMARY KEY,
		parent_id INTEGER NOT NULL DEFAULT 0,
		path_elem_id INTEGER NOT NULL DEFAULT 0,
		node_type INTEGER NOT NULL DEFAULT 0
	)`)
	if err != nil {
		return err
	}

	_, err = db.Exec(`CREATE INDEX IF NOT EXISTS path_elem_par_idx ON path (path_elem_id, parent_id)`)
	if err != nil {
		return err
	}

	_, err = db.Exec(`CREATE INDEX IF NOT EXISTS path_parent_idx ON path (parent_id)`)
	if err != nil {
		return err
	}

	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS file (
		id INTEGER PRIMARY KEY,
		path_id INTEGER NOT NULL DEFAULT 0,
		size INTEGER NOT NULL DEFAULT 0,
		mtime INTEGER NOT NULL DEFAULT 0,
		atime INTEGER NOT NULL DEFAULT 0,
		uid INTEGER NOT NULL DEFAULT 0,
		acqtime INTEGER NOT NULL DEFAULT 0
	)`)
	if err != nil {
		return err
	}

	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS simple_path_elem (
		id INTEGER PRIMARY KEY,
		simple_elem TEXT UNIQUE NOT NULL
	)`)
	if err != nil {
		return err
	}
	_, err = db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS simple_path_elem_unique_idx ON simple_path_elem (simple_elem COLLATE NOCASE)`)
	if err != nil {
		return err
	}

	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS simple_path_translate (
		id INTEGER PRIMARY KEY,
		simple_path_elem_id INTEGER NOT NULL DEFAULT 0,
		path_elem_id INTEGER NOT NULL DEFAULT 0
	)`)
	if err != nil {
		return err
	}
	_, err = db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS simple_path_translate_unique_idx ON simple_path_translate (simple_path_elem_id, path_elem_id)`)
	if err != nil {
		return err
	}

	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS dir (
		id INTEGER PRIMARY KEY,
		path_id INTEGER NOT NULL DEFAULT 0,
		file_count INTEGER NOT NULL DEFAULT 0,
		total_size INTEGER NOT NULL DEFAULT 0,
		acqtime_min INTEGER NOT NULL DEFAULT 0,
		acqtime_max INTEGER NOT NULL DEFAULT 0,
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
	)`)
	if err != nil {
		return err
	}
	_, err = db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS dir_path_unique_idx ON dir (path_id)`)
	if err != nil {
		return err
	}

	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS input_file (
		id INTEGER PRIMARY KEY,
		path_id INTEGER NOT NULL DEFAULT 0,
		timestamp INTEGER NOT NULL DEFAULT 0
	)`)
	if err != nil {
		return err
	}

	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS meta (
		name TEXT PRIMARY KEY,
		value INTEGER NOT NULL DEFAULT 0
	)`)
	if err != nil {
		return err
	}

	if err != nil {
		return err
	}
	_, err = db.Exec(`CREATE INDEX IF NOT EXISTS file_path_idx ON file (path_id)`)
	if err != nil {
		return err
	}
	_, err = db.Exec(`CREATE INDEX IF NOT EXISTS file_mtime_idx ON file (mtime)`)
	if err != nil {
		return err
	}
	_, err = db.Exec(`CREATE INDEX IF NOT EXISTS dir_path_idx ON dir (path_id)`)
	if err != nil {
		return err
	}

	return nil
}

func (d *DataFillerSqlite) Finalize() {
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
	if d.stmtInsertInputFile != nil {
		d.stmtInsertInputFile.Close()
	}
	if d.stmtSelectSimplePathElem != nil {
		d.stmtSelectSimplePathElem.Close()
	}
	if d.stmtInsertSimplePathElem != nil {
		d.stmtInsertSimplePathElem.Close()
	}
	if d.stmtCountSimplePathTranslate != nil {
		d.stmtCountSimplePathTranslate.Close()
	}
	if d.stmtInsertSimplePathTranslate != nil {
		d.stmtInsertSimplePathTranslate.Close()
	}
	if d.stmtSelectPathParentInfo != nil {
		d.stmtSelectPathParentInfo.Close()
	}
	if d.stmtCountFiles != nil {
		d.stmtCountFiles.Close()
	}
	if d.stmtDeleteDir != nil {
		d.stmtDeleteDir.Close()
	}
	if d.stmtSelectFileBatch != nil {
		d.stmtSelectFileBatch.Close()
	}
	if d.stmtBeginTransaction != nil {
		d.stmtBeginTransaction.Close()
	}
	if d.stmtCommitTransaction != nil {
		d.stmtCommitTransaction.Close()
	}
	if d.db != nil {
		d.db.Close()
	}
}
func simplifyPathElem(elem string) string {
	elem = strings.TrimSpace(elem)
	// TODO: remove TrimSuffix !!!
	elem = strings.TrimSuffix(elem, path.Ext(elem))
	elem = strings.TrimLeft(elem, "0")
	elem = strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			return r
		}
		return -1
	}, elem)
	return strings.ToLower(elem)
}

func (d *DataFillerSqlite) addSimplePathTranslate(pathElemID int64, pathElem string) error {
	simpleName := simplifyPathElem(pathElem)
	if simpleName == "" {
		return nil
	}
	var simpleID int64
	err := d.stmtSelectSimplePathElem.QueryRow(simpleName).Scan(&simpleID)
	if err == sql.ErrNoRows {
		res, err := d.stmtInsertSimplePathElem.Exec(simpleName)
		if err != nil {
			return err
		}
		simpleID, err = res.LastInsertId()
		if err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	var count int64
	if err := d.stmtCountSimplePathTranslate.QueryRow(simpleID, pathElemID).Scan(&count); err != nil {
		return err
	}
	if count == 0 {
		if _, err := d.stmtInsertSimplePathTranslate.Exec(simpleID, pathElemID); err != nil {
			return err
		}
	}
	return nil
}

func (d *DataFillerSqlite) sourceRootElems() []string {
	elems := make([]string, 0, 8)
	if d.dataSource != "" {
		elems = append(elems, d.dataSource)
	}
	if d.basePath == "" {
		return elems
	}
	base := strings.ReplaceAll(d.basePath, "\\", "/")
	base = strings.Trim(base, "/")
	if base == "" {
		return elems
	}
	base = strings.TrimPrefix(base, "/")
	if len(base) >= 2 && base[1] == ':' {
		elems = append(elems, base[:2])
		base = strings.TrimPrefix(base[2:], "/")
	} else if d.dataSource == "" && strings.Contains(base, "/") {
		parts := strings.Split(base, "/")
		root := strings.Join(parts[:2], "/")
		if root != "" {
			elems = append(elems, root)
			base = strings.TrimPrefix(strings.TrimPrefix(base, root), "/")
		}
	}
	if base == "" {
		return elems
	}
	for _, part := range strings.Split(base, "/") {
		if part != "" && part != "." {
			elems = append(elems, part)
		}
	}
	return elems
}

func (d *DataFillerSqlite) pathElems(dir string) []string {
	elems := d.sourceRootElems()
	trimmed := strings.TrimSpace(dir)
	if trimmed == "" || trimmed == "." {
		return elems
	}
	trimmed = strings.ReplaceAll(trimmed, "\\", "/")
	trimmed = strings.TrimLeft(trimmed, "/")
	for _, part := range strings.Split(trimmed, "/") {
		if part != "" && part != "." {
			elems = append(elems, part)
		}
	}
	return elems
}

func (d *DataFillerSqlite) addPathElems(elems []string) ([]int64, error) {
	out := make([]int64, 0, len(elems))
	for _, e := range elems {
		var id int64
		err := d.stmtSelectPathElem.QueryRow(e).Scan(&id)
		if err != nil {
			res, err2 := d.stmtInsertPathElem.Exec(e)
			if err2 != nil {
				return nil, err2
			}
			id, err2 = res.LastInsertId()
			if err2 != nil {
				return nil, err2
			}
			if err2 := d.addSimplePathTranslate(id, e); err2 != nil {
				return nil, err2
			}
		}
		out = append(out, id)
	}
	return out, nil
}

func (d *DataFillerSqlite) addFileDb(f dataprovider.FileInfo, pathId int64) (int64, error) {
	res, err := d.stmtInsertFile.Exec(pathId, f.Size, f.Mtime, f.Atime, f.Uid, d.acqTime)
	if err != nil {
		return 0, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	return id, nil
}

func (d *DataFillerSqlite) ensurePath(peId int64, parentId int64, nodeType int) (int64, error) {
	var id int64
	err := d.stmtSelectPath.QueryRow(peId, parentId, nodeType).Scan(&id)
	if err != nil {
		res, err2 := d.stmtInsertPath.Exec(peId, parentId, nodeType)
		if err2 != nil {
			return 0, err2
		}
		id, err2 = res.LastInsertId()
		if err2 != nil {
			return 0, err2
		}
	}
	return id, nil
}

type dirSummary struct {
	fileCount  int64
	totalSize  int64
	mTimeSize  [6]int64
	aTimeSize  [6]int64
	acqTimeMin int64
	acqTimeMax int64
}

var timeBins = []dataprovider.TimeBin{
	{MaxAgeS: 3600 * 24 * 30, Txt: "< 1 month"},
	{MaxAgeS: 3600 * 24 * 90, Txt: "1 to 3 months"},
	{MaxAgeS: 3600 * 24 * 365, Txt: "3 to 12 months "},
	{MaxAgeS: 3600 * 24 * 365 * 3, Txt: "1 to 3 years"},
	{MaxAgeS: 3600 * 24 * 365 * 5, Txt: "3-5 years"},
	{MaxAgeS: 3600 * 24 * 365 * 999, Txt: "> 5 years"},
}

func dirTimeBucket(t int64, acqTime int64) int {
	age := acqTime - t
	for i, tb := range timeBins {
		if age < int64(tb.MaxAgeS) {
			return i
		}
	}
	return len(timeBins) - 1
}

func (d *DataFillerSqlite) ancestorDirIDs(pathID int64) ([]int64, error) {
	ids := make([]int64, 0, 20)
	for current := pathID; current > 0; {
		var parentID, nodeType int64
		if pos, ok := d.ancestorDirIDCachePos[current]; ok {
			ids = append(ids, d.ancestorDirIDsCache[pos:]...)
			break
		}
		err := d.stmtSelectPathParentInfo.QueryRow(current).Scan(&parentID, &nodeType)
		if err != nil {
			return nil, err
		}
		if nodeType != nodeTypeFile {
			ids = append(ids, current)
		}
		if parentID <= 0 {
			break
		}
		current = parentID
	}
	d.ancestorDirIDCachePos = make(map[int64]int)
	for i, id := range ids {
		d.ancestorDirIDCachePos[id] = i
	}
	d.ancestorDirIDsCache = ids
	tmp := make([]int64, len(ids))
	copy(tmp, ids)
	slices.Reverse(tmp)
	return tmp, nil
}

func (d *DataFillerSqlite) flushDirSummaryBatch(stats map[int64]*dirSummary) error {
	if len(stats) == 0 {
		return nil
	}
	tx, err := d.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	stmt, err := tx.Prepare(`
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
			summary.acqTimeMin,
			summary.acqTimeMax,
			summary.mTimeSize[0],
			summary.mTimeSize[1],
			summary.mTimeSize[2],
			summary.mTimeSize[3],
			summary.mTimeSize[4],
			summary.mTimeSize[5],
			summary.aTimeSize[0],
			summary.aTimeSize[1],
			summary.aTimeSize[2],
			summary.aTimeSize[3],
			summary.aTimeSize[4],
			summary.aTimeSize[5],
		)
		if err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (d *DataFillerSqlite) SetSourceInfo(dataSource string, basePath string, acqTime int64) error {
	d.dataSource = dataSource
	d.basePath = basePath
	d.acqTime = acqTime
	d.ancestorDirIDsCache = nil
	d.ancestorDirIDCachePos = make(map[int64]int)
	d.prevDirElemsIds = nil
	d.prevDir = ""
	d.prevPathId = 0
	d.prevAncestorDirs = nil

	elems := d.sourceRootElems()
	if len(elems) == 0 {
		return nil
	}

	pathElems, err := d.addPathElems(elems)
	if err != nil {
		return err
	}

	parentId := int64(-1)
	for _, peID := range pathElems {
		id, err := d.ensurePath(peID, parentId, nodeTypeDir)
		if err != nil {
			return err
		}
		parentId = id
	}
	if parentId <= 0 {
		return nil
	}
	tx, err := d.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, err = tx.Exec(`WITH RECURSIVE subtree AS (SELECT id FROM path WHERE id = ? UNION ALL SELECT p.id FROM path p INNER JOIN subtree s ON p.parent_id = s.id) DELETE FROM file WHERE path_id IN (SELECT id FROM subtree)`, parentId)
	if err != nil {
		return err
	}
	_, err = tx.Exec(`WITH RECURSIVE subtree AS (SELECT id FROM path WHERE id = ? UNION ALL SELECT p.id FROM path p INNER JOIN subtree s ON p.parent_id = s.id) DELETE FROM dir WHERE path_id IN (SELECT id FROM subtree)`, parentId)
	if err != nil {
		return err
	}
	_, err = tx.Exec(`WITH RECURSIVE subtree AS (SELECT id FROM path WHERE id = ? UNION ALL SELECT p.id FROM path p INNER JOIN subtree s ON p.parent_id = s.id) DELETE FROM path WHERE id IN (SELECT id FROM subtree)`, parentId)
	if err != nil {
		return err
	}

	err = tx.Commit()
	if err != nil {
		return err
	}
	// Store the acquisition time in the input_file table to keep track of when this data was added
	if _, err := d.stmtInsertInputFile.Exec(parentId, d.acqTime); err != nil {
		return err
	}

	return nil
}

func (d *DataFillerSqlite) AddFile(f dataprovider.FileInfo) error {
	pathId := int64(-1)
	var elemsIds []int64
	var err error
	var fileId int64
	dir := path.Dir(f.Path)
	if dir == d.prevDir {
		pathId = d.prevPathId
		file := path.Base(f.Path)
		elems := []string{file}
		elemsIds, err = d.addPathElems(elems)
		if err != nil {
			return err
		}
		fileId = elemsIds[0]
	} else {
		elems := d.pathElems(f.Path)
		elemsIds, err = d.addPathElems(elems)
		if err != nil {
			return err
		}
		pathId = int64(-1)
		for _, peId := range elemsIds[:len(elemsIds)-1] {
			pathId, err = d.ensurePath(peId, pathId, nodeTypeDir)
			if err != nil {
				return err
			}
		}
		d.prevDir = dir
		d.prevPathId = pathId
		fileId = elemsIds[len(elemsIds)-1]
	}
	pathId, err = d.ensurePath(fileId, pathId, nodeTypeFile)
	if err != nil {
		return err
	}
	_, err = d.addFileDb(f, pathId)
	return err
}

func (d *DataFillerSqlite) RebuildDirTable(batchSize int, progress dataprovider.ProgressFunc) error {
	if batchSize <= 0 {
		batchSize = 1000
	}
	startedAt := time.Now().Unix()
	if _, err := d.db.Exec(`INSERT INTO meta(name, value) VALUES ('genTime', ?) ON CONFLICT(name) DO UPDATE SET value = excluded.value`, startedAt); err != nil {
		return err
	}
	now := time.Now().Unix()
	var totalRows int64
	if err := d.stmtCountFiles.QueryRow().Scan(&totalRows); err != nil {
		return err
	}
	if _, err := d.stmtDeleteDir.Exec(); err != nil {
		return err
	}
	stats := make(map[int64]*dirSummary)
	processedTotal := int64(0)
	for offset := 0; ; offset += batchSize {
		statsToFlush := make(map[int64]*dirSummary)
		rows, err := d.stmtSelectFileBatch.Query(batchSize, offset)
		if err != nil {
			return err
		}
		processed := false
		for rows.Next() {
			processed = true
			processedTotal++
			var pathID int64
			var ancestorPathID int64
			var size int64
			var mtime int64
			var atime int64
			var acqtime int64
			if err := rows.Scan(&pathID, &ancestorPathID, &size, &mtime, &atime, &acqtime); err != nil {
				rows.Close()
				return err
			}
			dirs, err := d.ancestorDirIDs(ancestorPathID)
			if err != nil {
				rows.Close()
				return err
			}
			for i, aDir := range d.prevAncestorDirs {
				if i >= len(dirs) || aDir != dirs[i] {
					statsToFlush[aDir] = stats[aDir]
				}
			}
			d.prevAncestorDirs = dirs
			for _, dirID := range dirs {
				sum := stats[dirID]
				if sum == nil {
					sum = &dirSummary{acqTimeMin: acqtime, acqTimeMax: acqtime}
					stats[dirID] = sum
				}
				sum.fileCount++
				sum.totalSize += size
				mtimeBucket := dirTimeBucket(mtime, now)
				sum.mTimeSize[mtimeBucket] += size
				atimeBucket := dirTimeBucket(atime, now)
				sum.aTimeSize[atimeBucket] += size
				if acqtime < sum.acqTimeMin || sum.acqTimeMin == 0 {
					sum.acqTimeMin = acqtime
				}
				if acqtime > sum.acqTimeMax {
					sum.acqTimeMax = acqtime
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
		if err := d.flushDirSummaryBatch(statsToFlush); err != nil {
			return err
		}
		for aDir := range statsToFlush {
			delete(stats, aDir)
		}
		if progress != nil {
			progress(processedTotal, totalRows)
		}
	}
	if err := d.flushDirSummaryBatch(stats); err != nil {
		return err
	}
	if progress != nil {
		progress(totalRows, totalRows)
	}
	return nil
}

func (d *DataFillerSqlite) StartTransaction() error {
	_, err := d.stmtBeginTransaction.Exec()
	return err
}

func (d *DataFillerSqlite) CommitTransaction() error {
	_, err := d.stmtCommitTransaction.Exec()
	return err
}
