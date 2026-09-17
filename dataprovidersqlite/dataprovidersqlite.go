package dataprovidersqlite

import (
	"database/sql"
	"path/filepath"
	"strings"
	"time"

	"github.com/524D/filelist2db/dataprovider"
	_ "modernc.org/sqlite"
)

// Define node types in path table
const (
	nodeTypeFile         = 0
	nodeTypeDir          = 1
	nodeTypeComputerName = 2
	nodeTypeShareName    = 3
)

// DataProviderSqlite implements the read-only DataProvider interface.
type DataProviderSqlite struct {
	db                    *sql.DB
	dataSource            string
	basePath              string
	acqTime               int64
	ancestorDirIDsCache   []int64
	ancestorDirIDCachePos map[int64]int
	// Prepared statements required for read-only queries.
	stmtSelectPathElem                    *sql.Stmt
	stmtSelectPath                        *sql.Stmt
	stmtSelectPathIDByElemAndParentPathID *sql.Stmt
	stmtSelectPathParentInfo              *sql.Stmt
	stmtSelectRootSources                 *sql.Stmt
	stmtSelectDirSummary                  *sql.Stmt
	stmtSelectSubDirs                     *sql.Stmt
	stmtSelectDirTotalSize                *sql.Stmt
	stmtCountFiles                        *sql.Stmt
	stmtDeleteDir                         *sql.Stmt
	stmtSelectFileBatch                   *sql.Stmt
}

func InitDataProviderSqlite(dbFile string) (dataprovider.DataProvider, error) {
	db, err := openReadOnlyDatabase(dbFile)
	if err != nil {
		return nil, err
	}

	d := DataProviderSqlite{db: db}
	if d.stmtSelectPathElem, err = db.Prepare(`SELECT id FROM path_elem WHERE elem = ?`); err != nil {
		return nil, err
	}
	if d.stmtSelectPath, err = db.Prepare(`SELECT id FROM path WHERE path_elem_id = ? AND parent_id = ? AND node_type = ?`); err != nil {
		return nil, err
	}
	if d.stmtSelectPathIDByElemAndParentPathID, err = db.Prepare(`SELECT id FROM path WHERE path_elem_id = ? AND parent_id = ?`); err != nil {
		return nil, err
	}
	if d.stmtSelectPathParentInfo, err = db.Prepare(`SELECT parent_id, node_type FROM path WHERE id = ?`); err != nil {
		return nil, err
	}
	if d.stmtSelectRootSources, err = db.Prepare(`SELECT pe.elem FROM path p JOIN path_elem pe ON pe.id = p.path_elem_id WHERE p.parent_id = -1 ORDER BY pe.elem`); err != nil {
		return nil, err
	}
	if d.stmtSelectDirSummary, err = db.Prepare(`SELECT mtime_size_1m, mtime_size_3m, mtime_size_1y, mtime_size_3y, mtime_size_5y, mtime_size_older,
		atime_size_1m, atime_size_3m, atime_size_1y, atime_size_3y, atime_size_5y, atime_size_older
		FROM dir WHERE path_id = ?`); err != nil {
		return nil, err
	}
	if d.stmtSelectSubDirs, err = db.Prepare(`SELECT pe.elem FROM path p JOIN path_elem pe ON pe.id = p.path_elem_id WHERE p.parent_id = ? AND p.node_type != 0 ORDER BY pe.elem`); err != nil {
		return nil, err
	}
	if d.stmtSelectDirTotalSize, err = db.Prepare(`SELECT total_size FROM dir WHERE path_id = ?`); err != nil {
		return nil, err
	}
	if d.stmtCountFiles, err = db.Prepare(`SELECT COUNT(*) FROM file`); err != nil {
		return nil, err
	}
	if d.stmtDeleteDir, err = db.Prepare(`DELETE FROM dir`); err != nil {
		return nil, err
	}
	if d.stmtSelectFileBatch, err = db.Prepare(`SELECT f.path_id, p.parent_id, f.size, f.mtime, f.atime
		FROM file f
		JOIN path p ON p.id = f.path_id
		ORDER BY f.id LIMIT ? OFFSET ?`); err != nil {
		return nil, err
	}
	return &d, nil
}

func openReadOnlyDatabase(dbFile string) (*sql.DB, error) {
	dsn := "file:" + filepath.ToSlash(dbFile) + "?_mode=ro"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	return db, nil
}

func (d *DataProviderSqlite) Finalize() {
	if d.stmtSelectPathElem != nil {
		d.stmtSelectPathElem.Close()
	}
	if d.stmtSelectPath != nil {
		d.stmtSelectPath.Close()
	}
	if d.stmtSelectPathIDByElemAndParentPathID != nil {
		d.stmtSelectPathIDByElemAndParentPathID.Close()
	}
	if d.stmtSelectPathParentInfo != nil {
		d.stmtSelectPathParentInfo.Close()
	}
	if d.stmtSelectRootSources != nil {
		d.stmtSelectRootSources.Close()
	}
	if d.stmtSelectDirSummary != nil {
		d.stmtSelectDirSummary.Close()
	}
	if d.stmtSelectSubDirs != nil {
		d.stmtSelectSubDirs.Close()
	}
	if d.stmtSelectDirTotalSize != nil {
		d.stmtSelectDirTotalSize.Close()
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
	if d.db != nil {
		d.db.Close()
	}
}

func simplifyPathElem(elem string) string {
	p := strings.Index(elem, ".")
	if p != -1 {
		elem = elem[:p]
	}
	elem = strings.TrimSpace(elem)
	elem = strings.TrimLeft(elem, "0")
	elem = strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			return r
		}
		return -1
	}, elem)
	return strings.ToLower(elem)
}

func (d *DataProviderSqlite) sourceRootElems() []string {
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

func (d *DataProviderSqlite) pathElems(dir string) []string {
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

func (d *DataProviderSqlite) SourceInfo() (string, string, int64) {
	return d.dataSource, d.basePath, d.acqTime
}

func (d *DataProviderSqlite) DataSources() ([]string, error) {
	rows, err := d.stmtSelectRootSources.Query()
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sources []string
	for rows.Next() {
		var elem string
		if err := rows.Scan(&elem); err != nil {
			return nil, err
		}
		sources = append(sources, elem)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return sources, nil
}

func (d *DataProviderSqlite) resolvePathID(dir string) (int64, error) {
	elems := d.pathElems(dir)
	if len(elems) == 0 {
		return 0, sql.ErrNoRows
	}

	parentID := int64(-1)
	for _, elem := range elems {
		var pathElemID int64
		if err := d.stmtSelectPathElem.QueryRow(elem).Scan(&pathElemID); err != nil {
			return 0, err
		}
		var id int64
		if err := d.stmtSelectPathIDByElemAndParentPathID.QueryRow(pathElemID, parentID).Scan(&id); err != nil {
			return 0, err
		}
		parentID = id
	}
	return parentID, nil
}

// Resolve the path ID for a directory under a given source root
func (d *DataProviderSqlite) resolvePathIDInSource(source string, dir string) (int64, error) {
	elems := []string{source}
	trimmed := strings.TrimSpace(dir)
	trimmed = strings.Trim(trimmed, "/")
	parts := strings.Split(trimmed, "/")
	for _, part := range parts {
		if part != "" {
			elems = append(elems, part)
		}
	}
	parentID := int64(-1)
	for _, elem := range elems {
		var pathElemID int64
		if err := d.stmtSelectPathElem.QueryRow(elem).Scan(&pathElemID); err != nil {
			return 0, err
		}
		var id int64
		if err := d.stmtSelectPathIDByElemAndParentPathID.QueryRow(pathElemID, parentID).Scan(&id); err != nil {
			return 0, err
		}
		parentID = id
	}
	// Parent ID now contains the path ID for the desired directory under the source root
	return parentID, nil
}

func (d *DataProviderSqlite) DirExists(source string, dir string) (bool, error) {
	_, err := d.resolvePathIDInSource(source, dir)
	if err == nil {
		return true, nil
	}
	if err == sql.ErrNoRows {
		return false, nil
	}
	return false, err
}

var timeBins = []dataprovider.TimeBin{
	{MaxAgeS: (3600 * 24 * 30), Txt: "< 1 month"},
	{MaxAgeS: (3600 * 24 * 90), Txt: "1 to 3 months"},
	{MaxAgeS: (3600 * 24 * 365), Txt: "3 to 12 months "},
	{MaxAgeS: (3600 * 24 * 365 * 3), Txt: "1 to 3 years"},
	{MaxAgeS: (3600 * 24 * 365 * 5), Txt: "3-5 years"},
	{MaxAgeS: (3600 * 24 * 365 * 999), Txt: "> 5 years"},
}

func (d *DataProviderSqlite) DirSizeTimeBins(source string, dir string) ([]uint64, []uint64, []dataprovider.TimeBin, error) {
	pathID, err := d.resolvePathIDInSource(source, dir)
	if err != nil {
		return nil, nil, timeBins, err
	}
	// Obtain all time bins for the directory in a single query, to avoid multiple queries and improve performance
	var mSizes = make([]uint64, 6)
	var aSizes = make([]uint64, 6)
	if err := d.stmtSelectDirSummary.QueryRow(pathID).Scan(&mSizes[0], &mSizes[1], &mSizes[2], &mSizes[3], &mSizes[4], &mSizes[5],
		&aSizes[0], &aSizes[1], &aSizes[2], &aSizes[3], &aSizes[4], &aSizes[5]); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil, timeBins, nil
		}
		return nil, nil, timeBins, err
	}
	return mSizes, aSizes, timeBins, nil
}

func (d *DataProviderSqlite) SubDirs(source string, dir string) ([]string, error) {
	pathID, err := d.resolvePathIDInSource(source, dir)
	if err != nil {
		return nil, err
	}
	rows, err := d.stmtSelectSubDirs.Query(pathID)
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

func (d *DataProviderSqlite) SubDirSize(source string, dir string) (uint64, error) {
	pathID, err := d.resolvePathIDInSource(source, dir)
	if err != nil {
		return 0, err
	}
	var totalSize int64
	if err := d.stmtSelectDirTotalSize.QueryRow(pathID).Scan(&totalSize); err != nil {
		if err == sql.ErrNoRows {
			return 0, nil
		}
		return 0, err
	}
	return uint64(totalSize), nil
}

type SameFiles struct {
	Files []dataprovider.FileInfo
}

func (d *DataProviderSqlite) FindSameFiles(minSize uint64, minTimeDiff int64, maxTimeDiff int64) ([]SameFiles, error) {
	// Find files with same size and same mtime
	// Return slice of SameFiles

	return nil, nil
}

func (d *DataProviderSqlite) resolvePathByID(pathID int64) (string, error) {
	if pathID <= 0 {
		return "", nil
	}

	parts := make([]string, 0, 8)
	for current := pathID; current > 0; {
		var parentID int64
		var elem string
		if err := d.db.QueryRow(`SELECT p.parent_id, pe.elem FROM path p JOIN path_elem pe ON pe.id = p.path_elem_id WHERE p.id = ?`, current).Scan(&parentID, &elem); err != nil {
			return "", err
		}
		parts = append(parts, elem)
		current = parentID
	}

	for i, j := 0, len(parts)-1; i < j; i, j = i+1, j-1 {
		parts[i], parts[j] = parts[j], parts[i]
	}
	return strings.Join(parts, "/"), nil
}

// TODO: The function below is generated by Copilot (MAI-Code1.1). Need to check if its correct!
func (d *DataProviderSqlite) Search(selection dataprovider.SearchSelection) ([]dataprovider.SearchResult, map[string]interface{}, error) {
	start := time.Now()
	meta := func() map[string]interface{} {
		return map[string]interface{}{"SearchTimeMicroSeconds": time.Since(start).Microseconds()}
	}

	limit := selection.ResultsLimit
	if limit <= 0 || limit > 20 {
		limit = 20
	}
	first := selection.ResultsFirst
	if first < 0 {
		first = 0
	}

	pathValid := strings.TrimSpace(selection.Path) != ""
	kindSet := selection.Kind >= 0
	sizeMinValid := selection.SizeMin >= 0
	sizeMaxValid := selection.SizeMax >= 0
	mtimeMinValid := selection.MtimeMin >= 0
	mtimeMaxValid := selection.MtimeMax >= 0
	atimeMinValid := selection.AtimeMin >= 0
	atimeMaxValid := selection.AtimeMax >= 0

	allOthersInvalid := !kindSet && !sizeMinValid && !sizeMaxValid && !mtimeMinValid && !mtimeMaxValid && !atimeMinValid && !atimeMaxValid
	if pathValid && selection.SimplePath && allOthersInvalid {
		results, _, err := d.SearchBySimpleName(selection.Path, int(limit))
		if err != nil {
			return nil, meta(), err
		}
		if first > 0 && len(results) > int(first) {
			results = results[first:]
		}
		return results, meta(), nil
	}

	sizeOnly := !pathValid && !kindSet && !mtimeMinValid && !mtimeMaxValid && !atimeMinValid && !atimeMaxValid &&
		sizeMinValid && sizeMaxValid
	if sizeOnly {
		query := `
			SELECT kind, path_id, size, mtime, atime, file_count, total_size
			FROM (
				SELECT 'file' AS kind, f.path_id AS path_id, f.size AS size, f.mtime AS mtime, f.atime AS atime, 0 AS file_count, 0 AS total_size
				FROM file AS f
				WHERE f.size >= ? AND f.size <= ?
				UNION ALL
				SELECT 'directory' AS kind, d.path_id AS path_id, d.total_size AS size, 0 AS mtime, 0 AS atime, d.file_count AS file_count, d.total_size AS total_size
				FROM dir AS d
				WHERE d.total_size >= ? AND d.total_size <= ?
			)
			ORDER BY path_id
			LIMIT ? OFFSET ?`
		rows, err := d.db.Query(query, selection.SizeMin, selection.SizeMax, selection.SizeMin, selection.SizeMax, limit, first)
		if err != nil {
			return nil, meta(), err
		}
		defer rows.Close()

		results := make([]dataprovider.SearchResult, 0, limit)
		for rows.Next() {
			var kind string
			var pathID int64
			var size int64
			var mtime int64
			var atime int64
			var fileCount int64
			var totalSize int64
			if err := rows.Scan(&kind, &pathID, &size, &mtime, &atime, &fileCount, &totalSize); err != nil {
				return nil, meta(), err
			}
			path, err := d.resolvePathByID(pathID)
			if err != nil {
				return nil, meta(), err
			}
			results = append(results, dataprovider.SearchResult{
				Kind:      kind,
				Path:      path,
				Size:      uint64(size),
				Mtime:     mtime,
				Atime:     atime,
				FileCount: fileCount,
				TotalSize: uint64(totalSize),
			})
		}
		if err := rows.Err(); err != nil {
			return nil, meta(), err
		}
		return results, meta(), nil
	}

	fileWhere := " WHERE 1=1"
	dirWhere := " WHERE 1=1"
	args := make([]any, 0, 16)

	if pathValid {
		if selection.SimplePath {
			simpleTerm := simplifyPathElem(selection.Path)
			if simpleTerm != "" {
				fileWhere += ` AND EXISTS (SELECT 1 FROM simple_path_translate spt JOIN simple_path_elem spe ON spe.id = spt.simple_path_elem_id WHERE spt.path_elem_id = pe.id AND spe.simple_elem LIKE ?)`
				dirWhere += ` AND EXISTS (SELECT 1 FROM simple_path_translate spt JOIN simple_path_elem spe ON spe.id = spt.simple_path_elem_id WHERE spt.path_elem_id = pe.id AND spe.simple_elem LIKE ?)`
				args = append(args, simpleTerm+"%", simpleTerm+"%")
			}
		} else {
			fileWhere += ` AND LOWER(pe.elem) LIKE ?`
			dirWhere += ` AND LOWER(pe.elem) LIKE ?`
			args = append(args, "%"+strings.ToLower(strings.TrimSpace(selection.Path))+"%", "%"+strings.ToLower(strings.TrimSpace(selection.Path))+"%")
		}
	}
	if selection.Kind == 0 {
		// zero-value Kind is treated as unset in generic selector searches.
	} else if selection.Kind == 1 {
		dirWhere += " AND 0=1"
	} else if selection.Kind == 2 {
		fileWhere += " AND 0=1"
	}
	if sizeMinValid {
		fileWhere += " AND f.size >= ?"
		dirWhere += " AND d.total_size >= ?"
		args = append(args, selection.SizeMin, selection.SizeMin)
	}
	if sizeMaxValid {
		fileWhere += " AND f.size <= ?"
		dirWhere += " AND d.total_size <= ?"
		args = append(args, selection.SizeMax, selection.SizeMax)
	}
	if mtimeMinValid {
		fileWhere += " AND f.mtime >= ?"
		args = append(args, selection.MtimeMin)
	}
	if mtimeMaxValid {
		fileWhere += " AND f.mtime <= ?"
		args = append(args, selection.MtimeMax)
	}
	if atimeMinValid {
		fileWhere += " AND f.atime >= ?"
		args = append(args, selection.AtimeMin)
	}
	if atimeMaxValid {
		fileWhere += " AND f.atime <= ?"
		args = append(args, selection.AtimeMax)
	}

	query := `
		SELECT kind, path_id, size, mtime, atime, file_count, total_size
		FROM (
			SELECT 'file' AS kind, f.path_id AS path_id, f.size AS size, f.mtime AS mtime, f.atime AS atime, 0 AS file_count, 0 AS total_size
			FROM file AS f
			JOIN path AS p ON p.id = f.path_id
			JOIN path_elem AS pe ON pe.id = p.path_elem_id` + fileWhere + `
			UNION ALL
			SELECT 'directory' AS kind, d.path_id AS path_id, d.total_size AS size, 0 AS mtime, 0 AS atime, d.file_count AS file_count, d.total_size AS total_size
			FROM dir AS d
			JOIN path AS p ON p.id = d.path_id
			JOIN path_elem AS pe ON pe.id = p.path_elem_id` + dirWhere + `
		)
		ORDER BY path_id
		LIMIT ? OFFSET ?`
	args = append(args, limit, first)

	rows, err := d.db.Query(query, args...)
	if err != nil {
		return nil, meta(), err
	}
	defer rows.Close()

	results := make([]dataprovider.SearchResult, 0, limit)
	for rows.Next() {
		var kind string
		var pathID int64
		var size int64
		var mtime int64
		var atime int64
		var fileCount int64
		var totalSize int64
		if err := rows.Scan(&kind, &pathID, &size, &mtime, &atime, &fileCount, &totalSize); err != nil {
			return nil, meta(), err
		}
		path, err := d.resolvePathByID(pathID)
		if err != nil {
			return nil, meta(), err
		}
		results = append(results, dataprovider.SearchResult{
			Kind:      kind,
			Path:      path,
			Size:      uint64(size),
			Mtime:     mtime,
			Atime:     atime,
			FileCount: fileCount,
			TotalSize: uint64(totalSize),
		})
	}
	if err := rows.Err(); err != nil {
		return nil, meta(), err
	}
	return results, meta(), nil
}

func (d *DataProviderSqlite) SearchByName(name string, limit int) ([]dataprovider.SearchResult, map[string]interface{}, error) {
	start := time.Now()
	meta := func() map[string]interface{} {
		return map[string]interface{}{"SearchTimeMicroSeconds": time.Since(start).Microseconds()}
	}

	term := strings.TrimSpace(name)
	if len(term) < 4 {
		return nil, meta(), nil
	}
	if limit <= 0 || limit > 20 {
		limit = 20
	}

	query := "%" + strings.ToLower(term) + "%"

	rows, err := d.db.Query(`
        SELECT kind, path_id, size, mtime, atime, file_count, total_size
        FROM (
            SELECT 'file' AS kind, f.path_id AS path_id, f.size AS size, f.mtime AS mtime, f.atime AS atime, 0 AS file_count, 0 AS total_size
            FROM file AS f
            JOIN path AS p ON p.id = f.path_id
            JOIN path_elem AS pe ON pe.id = p.path_elem_id
            WHERE LOWER(pe.elem) LIKE ?
            UNION ALL
            SELECT 'directory' AS kind, d.path_id AS path_id, d.total_size AS size, 0 AS mtime, 0 AS atime, d.file_count AS file_count, d.total_size AS total_size
            FROM dir AS d
            JOIN path AS p ON p.id = d.path_id
            JOIN path_elem AS pe ON pe.id = p.path_elem_id
            WHERE LOWER(pe.elem) LIKE ?
        )
        ORDER BY path_id
        LIMIT ?`, query, query, limit)
	if err != nil {
		return nil, meta(), err
	}
	defer rows.Close()

	results := make([]dataprovider.SearchResult, 0, limit)
	for rows.Next() {
		var kind string
		var pathID int64
		var size int64
		var mtime int64
		var atime int64
		var fileCount int64
		var totalSize int64

		if err := rows.Scan(&kind, &pathID, &size, &mtime, &atime, &fileCount, &totalSize); err != nil {
			return nil, meta(), err
		}

		path, err := d.resolvePathByID(pathID)
		if err != nil {
			return nil, meta(), err
		}

		results = append(results, dataprovider.SearchResult{
			Kind:      kind,
			Path:      path,
			Size:      uint64(size),
			Mtime:     mtime,
			Atime:     atime,
			FileCount: fileCount,
			TotalSize: uint64(totalSize),
		})
	}

	if err := rows.Err(); err != nil {
		return nil, meta(), err
	}
	return results, meta(), nil
}

func (d *DataProviderSqlite) SearchBySimpleName(name string, limit int) ([]dataprovider.SearchResult, map[string]interface{}, error) {
	start := time.Now()
	meta := func() map[string]interface{} {
		return map[string]interface{}{"SearchTimeMicroSeconds": time.Since(start).Microseconds()}
	}

	term := strings.TrimSpace(name)
	if len(term) == 0 {
		return nil, meta(), nil
	}
	if limit <= 0 || limit > 20 {
		limit = 20
	}

	simpleTerm := simplifyPathElem(term)
	if simpleTerm == "" {
		return nil, meta(), nil
	}
	prefixTerm := simpleTerm + "%"

	rows, err := d.db.Query(`
        SELECT kind, path_id, size, mtime, atime, file_count, total_size
        FROM (
            SELECT 'file' AS kind, f.path_id AS path_id, f.size AS size, f.mtime AS mtime, f.atime AS atime, 0 AS file_count, 0 AS total_size
            FROM file AS f
            JOIN path AS p ON p.id = f.path_id
            JOIN path_elem AS pe ON pe.id = p.path_elem_id
            JOIN simple_path_translate AS spt ON spt.path_elem_id = pe.id
            JOIN simple_path_elem AS spe ON spe.id = spt.simple_path_elem_id
            WHERE spe.simple_elem LIKE ?
            UNION ALL
            SELECT 'directory' AS kind, d.path_id AS path_id, d.total_size AS size, 0 AS mtime, 0 AS atime, d.file_count AS file_count, d.total_size AS total_size
            FROM dir AS d
            JOIN path AS p ON p.id = d.path_id
            JOIN path_elem AS pe ON pe.id = p.path_elem_id
            JOIN simple_path_translate AS spt ON spt.path_elem_id = pe.id
            JOIN simple_path_elem AS spe ON spe.id = spt.simple_path_elem_id
            WHERE spe.simple_elem LIKE ?
        )
        ORDER BY path_id
        LIMIT ?`, prefixTerm, prefixTerm, limit)
	if err != nil {
		return nil, meta(), err
	}
	defer rows.Close()

	results := make([]dataprovider.SearchResult, 0, limit)
	for rows.Next() {
		var kind string
		var pathID int64
		var size int64
		var mtime int64
		var atime int64
		var fileCount int64
		var totalSize int64

		if err := rows.Scan(&kind, &pathID, &size, &mtime, &atime, &fileCount, &totalSize); err != nil {
			return nil, meta(), err
		}

		path, err := d.resolvePathByID(pathID)
		if err != nil {
			return nil, meta(), err
		}

		results = append(results, dataprovider.SearchResult{
			Kind:      kind,
			Path:      path,
			Size:      uint64(size),
			Mtime:     mtime,
			Atime:     atime,
			FileCount: fileCount,
			TotalSize: uint64(totalSize),
		})
	}

	if err := rows.Err(); err != nil {
		return nil, meta(), err
	}
	return results, meta(), nil
}
