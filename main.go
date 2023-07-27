package main

import (
	"bufio"
	"database/sql"
	"errors"
	"flag"
	"io"
	"net/url"
	"os"
	"path"
	"regexp"
	"strconv"
	"time"

	_ "modernc.org/sqlite"
)

type Args struct {
	dbFile string
}

type uidT uint64

type fileInfo struct {
	path       string
	size       uint64
	uid        uidT
	mtime      int64
	atime      int64
	atimeValid bool
}

var args Args

type timeBin struct {
	maxAgeS uint64 // Maximum age in seconds
	txt     string // Textual description of time bin
}

var timeBins = [...]timeBin{
	{(3600 * 24 * 30), "< 1 month"},
	{(3600 * 24 * 90), "1 to 3 months"},
	{(3600 * 24 * 365), "3 to 12 months "},
	{(3600 * 24 * 365 * 3), "1 to 3 years"},
	{(3600 * 24 * 365 * 5), "3-5 years"},
	{(3600 * 24 * 30) * 999, "> 5 years"},
}

func parseCmdLine() []string {
	// Parse command line arguments
	// -db <database>: name of sqlite database to use (default: "db.sqlite")
	// Remaining arguments: file lists to process

	// If no arguments, print usage and exit
	// If -h or --help, print usage and exit

	flag.StringVar(&args.dbFile, "db", "db.sqlite", "name of sqlite database to use")
	flag.Parse()
	return flag.Args()
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

	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS file (
		id INTEGER PRIMARY KEY,
		size INTEGER,
		mtime INTEGER,
		atime INTEGER,
		uid INTEGER,
	)`)
	if err != nil {
		return err
	}

	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS file_path (
		id INTEGER PRIMARY KEY,
		path_elem_id INTEGER,
		file_id INTEGER,
		depth INTEGER,
		FOREIGN KEY(path_elem_id) REFERENCES path_elem(id),
		FOREIGN KEY(file_id) REFERENCES file(id)
	)`)
	if err != nil {
		return err
	}

	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS dir (
		id INTEGER PRIMARY KEY,
		parent_id INTEGER,
		tot_size INTEGER,
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
		atime_size_older INTEGER,
		FOREIGN KEY(parent_id) REFERENCES dir(id),
	)`)
	if err != nil {
		return err
	}

	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS dir_path (
		id INTEGER PRIMARY KEY,
		path_elem_id INTEGER,
		dir_id INTEGER,
		depth INTEGER,
		FOREIGN KEY(path_elem_id) REFERENCES path_elem(id),
		FOREIGN KEY(dir_id) REFERENCES dir(id)
	)`)
	if err != nil {
		return err
	}

	return nil
}

// Decode computer name, base path and search timestamp from filename of "find" result
// The filename should be named like:
// [_computername_]<escaped_base_path>_<timestamp>.<txt|lst>
// Where:
//   - computername is the name of the computer whose files are indexed. Can
//     be absent if the filepath refers to a network share
//   - escaped_base_path is the path under which the file info is obtained
//   - timestamp is the timestamp of the scan, in the format YYYYMMDD-HHMMSS
//
// Since the path name contains characters that cannot be part of a filename,
// it is escaped:
// A leading underscore ("_") is replaced by "%5F"
// Other invalid filename characters are replaced by "%" followed by their ASCII/UTF8
// hex character code, e.g. "%2F" for slash ("/") and "%5C" for backslash ("\").
// "%" is replaced by %25
func decodeFindFilename(fn string) (string, string, int64, error) {
	re := regexp.MustCompile(`^(?:.*[/\\])?(?:_([^/\\]*?)_)?([^/\\]*?)_([0-9\-]+)\.lst|\.txt$`)
	match := re.FindStringSubmatch(fn)
	if match == nil {
		return ``, ``, 0, errors.New("can't extract basepath/timestamp from filename")
	}
	basePath, err := url.QueryUnescape(match[2])
	if err != nil {
		return ``, ``, 0, errors.New("can't decode basepath from filename")
	}
	ts, err := time.Parse(`20060102-150405`, match[3])
	if err != nil {
		return ``, ``, 0, errors.New("can't decode timestamp from filename")
	}
	t := ts.Unix()

	return match[1], basePath, t, nil
}

func binTime(t int64, acqTime int64) int {
	age := acqTime - t
	for i, tb := range timeBins {
		if age < int64(tb.maxAgeS) {
			return i
		}
	}
	return len(timeBins) - 1
}

func sumSz(sz [len(timeBins)]uint64) uint64 {
	sum := uint64(0)
	for i := 0; i < len(timeBins); i++ {
		sum += sz[i]
	}
	return sum
}

type dataProvider interface {
	SetSourceInfo(computerName string, basePath string, acqTime int64) error
	SourceInfo() (string, string, int64)
	AddFile(fileInfo) error
}

type dataProviderSqlite struct {
	db           *sql.DB
	computerName string
	basePath     string
	acqTime      int64
}

func (d *dataProviderSqlite) SetSourceInfo(computerName string, basePath string, acqTime int64) error {
	d.computerName = computerName
	d.basePath = basePath
	d.acqTime = acqTime
	return nil
}

func (d *dataProviderSqlite) SourceInfo() (string, string, int64) {
	return d.computerName, d.basePath, d.acqTime
}

func splitPath(fn string) []string {
	// split path in elements
	elems := make([]string, 0, 10)
	d := fn
	for ; d != ``; d, fn = path.Split(d) {
		elems = append(elems, fn)
	}
	// Reverse slice
	// FIXME: use slices.Reverse(s) when Go 1.21 is released
	for i, j := 0, len(elems)-1; i < j; i, j = i+1, j-1 {
		elems[i], elems[j] = elems[j], elems[i]
	}
	return elems
}

func (d *dataProviderSqlite) addPathElems(elems []string) ([]int64, error) {
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

func (d *dataProviderSqlite) addFile(f fileInfo) (int64, error) {
	// Add file to file table
	// Return id of file
	var id int64
	res, err := d.db.Exec(`INSERT INTO file (size, mtime, atime, uid) VALUES (?, ?, ?, ?)`, f.size, f.mtime, f.atime, f.uid)
	if err != nil {
		return 0, err
	}
	id, err = res.LastInsertId()
	if err != nil {
		return 0, err
	}
	return id, err
}

func (d *dataProviderSqlite) addFilePath(f fileInfo, id int64, elemsIds []int64) error {
	// Add path elements to file_path table
	for i, peId := range elemsIds {
		_, err := d.db.Exec(`INSERT INTO file_path (path_elem_id, file_id, depth) VALUES (?, ?, ?)`, peId, id, i)
		if err != nil {
			return err
		}
	}
	return nil
}

func (d *dataProviderSqlite) addDirPath(f fileInfo, id int64, elemsIds []int64) error {
	// Add path elements to dir_path table
	for i, peId := range elemsIds {
		_, err := d.db.Exec(`INSERT INTO dir_path (path_elem_id, dir_id, depth) VALUES (?, ?, ?)`, peId, id, i)
		if err != nil {
			return err
		}
	}
	return nil
}

func (d *dataProviderSqlite) AddFile(f fileInfo) error {
	// split path in elements
	elems := splitPath(f.path)

	// TODO: handle situation where file is already present

	// add elements to path_elem table
	elemsIds, err := d.addPathElems(elems)
	if err != nil {
		return err
	}
	// add file to file table
	fileId, err := d.addFile(f)
	if err != nil {
		return err
	}
	// add path elements to file_path table
	err = d.addFilePath(f, fileId, elemsIds)
	if err != nil {
		return err
	}

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

func parseFileList(d dataProvider, reader io.Reader) error {
	// parseFileList parses a file list and adds the files to the data provider
	// File info e.g. generated by:
	// find ${DIR} -type f -printf '%s\t%U\t%i\t%n\t%T@\t%A@\t%C@\t%P\n'
	// Regex to parse file info. We discard fractional part of timestamp.
	re := regexp.MustCompile(`^([0-9]*)\t([0-9]*)\t([0-9]*)\t([0-9]*)\t([0-9]*)(?:\.[0-9]*)\t([0-9]*)(?:\.[0-9]*)\t([0-9]*)(?:\.[0-9]*)\t(.*)$`)

	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		l := scanner.Text()
		m := re.FindStringSubmatch(l)
		if m == nil || len(m) < 9 || m[8] == `` {
			// Parsing line failed
		} else {
			var f fileInfo
			f.size, _ = strconv.ParseUint(m[1], 10, 64)
			uidNum, _ := strconv.ParseUint(m[2], 10, 64)
			f.uid = uidT(uidNum)
			f.mtime, _ = strconv.ParseInt(m[5], 10, 64)
			f.atime, _ = strconv.ParseInt(m[6], 10, 64)
			f.path = m[8]
			err := d.AddFile(f)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func processListFile(d dataProvider, fn string) error {
	computerName, basePath, acqTime, err := decodeFindFilename(fn)
	if err != nil {
		return err
	}
	d.SetSourceInfo(computerName, basePath, acqTime)
	f, err := os.Open(fn)
	if err != nil {
		return err
	}
	defer f.Close()

	return parseFileList(d, f)
}

func InitDataProviderSqlite(dbFile string) (dataProvider, error) {
	// Open database
	db, err := openDatabase(dbFile)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	// Create tables if they don't exist
	createTables(db)

	d := dataProviderSqlite{db: db}

	return &d, nil
}

func main() {
	// Parse command line arguments
	files := parseCmdLine()

	// Create data provider
	d, err := InitDataProviderSqlite(args.dbFile)
	if err != nil {
		panic(err)
	}

	// Process files
	for _, fn := range files {
		err = processListFile(d, fn)
		if err != nil {
			panic(err)
		}
	}
}

/*
package main

import (
	"bufio"
	"embed"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"net/url"
	"os"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// This program generates info about folders (directories)
// Input to this program is a list of files with associated metadata (see below)
// The program is run as:
// storageprobe -filelist <filename> -servedir <webdir>
// The program will start a webserver on port 3000, serving on the webdir directory a web page with
// a pie chart showing the size of subdirectories.
// E.g. storageprobe -filelist _cpm-calc14_D%3A%5CData.lst

// The input file for this program is generated by the "find" command,
// and the input filename is used to encode the computer name, base path and timestamp of the data
// The file with "find" output can be generated by the following script:
/*
#!/bin/bash
T1=`date +%Y%m%d-%H%M%S`
FINDPRINTF='%s\t%U\t%i\t%n\t%T@\t%A@\t%C@\t%P\n'
MOUNTPATH='/whatever/'
STARTPATH='somedir'
mkdir -p "${T1}"
cd "${T1}"
touch meta.txt
echo T1="'${T1}'" >> meta.txt
echo FINDPRINTF="'${FINDPRINTF}'" >> meta.txt
echo MOUNTPATH="'${MOUNTPATH}'" >> meta.txt
echo STARTPATH="'${STARTPATH}'" >> meta.txt
find "${MOUNTPATH}${STARTPATH}" -type f -printf "${FINDPRINTF}" > "${STARTPATH}.lst" 2> errors.txt
T2=`date +%Y%m%d-%H%M%S`
echo T2="'${T2}'" >> meta.txt
touch done.txt
*/

/*
const MaxPieParts = 10 // Maximum subdirectories to show in pie chart, rest is combined under "Other"
const GigaByte = 1000000000

//go:embed web/*
var www embed.FS

type subDir struct {
	name string
	size uint64
}
type legendDataJson struct {
	Name string `json:"name"`
}

type dirForJson struct {
	ComputerName string           `json:"computerName"`
	BasePath     string           `json:"basePath"`
	AcqTime      int64            `json:"acqTime"`
	Title        string           `json:"title"`
	Categories   []string         `json:"categories"`
	LegendData   []legendDataJson `json:"legendData"`
	DataAcc      []float64        `json:"dataAcc"`
	DataMod      []float64        `json:"dataMod"`
	PieTitle     string           `json:"pieTitle"`
	PieSubTitle  string           `json:"pieSubTitle"`
	PieData      []interface{}    `json:"pieData"`
}

type timeBin struct {
	maxAgeS uint64 // Maximum age in seconds
	txt     string // Textual description of time bin
}

var timeBins = [...]timeBin{
	{(3600 * 24 * 30), "< 1 month"},
	{(3600 * 24 * 90), "1 to 3 months"},
	{(3600 * 24 * 365), "3 to 12 months "},
	{(3600 * 24 * 365 * 3), "1 to 3 years"},
	{(3600 * 24 * 365 * 5), "3-5 years"},
	{(3600 * 24 * 30) * 999, "> 5 years"},
}

type uidT uint64

type fileInfo struct {
	path       string
	size       uint64
	uid        uidT
	mtime      int64
	atime      int64
	atimeValid bool
}

type dirInfo struct {
	path        string     // path, starting at root of file list
	files       []fileInfo // slice with files directly in this dir
	dirs        []dirInfo  // Slice with subdirectories
	uids        []uidT     // Slice with numerical UIDs of owners
	mtimeLatest int64      // mtime of newest file (recursively) in this dir
	atimeLatest int64      // atime of newest file (recursively) in this dir
	atimeValid  bool       // is atime valid
}

type void struct{}

var member void

type dirSummary struct {
	dirs      map[string]void       // Map with subdirectories
	uids      map[uidT]void         // Map with UIDs of owners (recursive)
	sizeModTm [len(timeBins)]uint64 // Array with size modified before timestamp
	sizeAccTm [len(timeBins)]uint64 // Array with size accessed before timestamp
}

type dataProvider struct {
	dirSummap    map[string]dirSummary
	computerName string
	basePath     string
	acqTime      int64
}

func (d *dataProvider) dirExists(dir string) bool {
	_, exists := d.dirSummap[dir]
	return exists
}

func (d *dataProvider) dirSizeModTimeBin(dir string, bin int) uint64 {
	return d.dirSummap[dir].sizeModTm[bin]
}

func (d *dataProvider) dirSizeAccTimeBin(dir string, bin int) uint64 {
	return d.dirSummap[dir].sizeAccTm[bin]
}

func (d *dataProvider) subDirs(dir string) []string {
	subDirs := make([]string, 0, len(d.dirSummap[dir].dirs))
	for sd := range d.dirSummap[dir].dirs {
		subDirs = append(subDirs, sd)
	}
	return subDirs
}

func (d *dataProvider) subDirSize(dir string) uint64 {
	return sumSz(d.dirSummap[dir].sizeAccTm)
}

func (d *dataProvider) SourceInfo() (string, string, int64) {
	return d.computerName, d.basePath, d.acqTime
}

func (d *dataProvider) SetSourceInfo(computerName string, basePath string, acqTime int64) {
	d.computerName = computerName
	d.basePath = basePath
	d.acqTime = acqTime
}

func (d *dataProvider) addFileInfo(f fileInfo) error {
	accTimeBin := binTime(f.atime, d.acqTime)
	modTimeBin := binTime(f.mtime, d.acqTime)
	lastDir := ``
	for dir := path.Dir(f.path); lastDir != `.` && lastDir != `/`; dir = path.Dir(dir) {
		var ds dirSummary
		var ok bool
		// Create dir summary item if this is the first time we encountered this dir
		if ds, ok = d.dirSummap[dir]; !ok {
			ds.dirs = make(map[string]void)
			ds.uids = make(map[uidT]void)
		}

		if lastDir != `` {
			ds.dirs[lastDir] = member
		}
		ds.sizeAccTm[accTimeBin] += f.size
		ds.sizeModTm[modTimeBin] += f.size
		ds.uids[uidT(f.uid)] = member
		d.dirSummap[dir] = ds
		lastDir = path.Base(dir)
	}
	return nil
}

// Decode computer name, base path and search timestamp from filename of "find" result
// The filename should be named like:
// [_computername_]<escaped_base_path>_<timestamp>.<txt|lst>
// Where:
//   - computername is the name of the computer whose files are indexed. Can
//     be absent if the filepath refers to a network share
//   - escaped_base_path is the path under which the file info is obtained
//   - timestamp is the timestamp of the scan, in the format YYYYMMDD-HHMMSS
//
// Since the path name contains characters that cannot be part of a filename,
// it is escaped:
// A leading underscore ("_") is replaces by "%5F"
// Other invalid filename characters are replaced by "%"" followed by their ASCII/UTF8
// hex character code.
// "%" is replaced by %25
func decodeFindFilename(fn string) (string, string, int64, error) {
	re := regexp.MustCompile(`^(?:.*[/\\])?(?:_([^/\\]*?)_)?([^/\\]*?)_([0-9\-]+)\.lst|\.txt$`)
	match := re.FindStringSubmatch(fn)
	if match == nil {
		return ``, ``, 0, errors.New("can't extract basepath/timestamp from filename")
	}
	basePath, err := url.QueryUnescape(match[2])
	if err != nil {
		return ``, ``, 0, errors.New("can't decode basepath from filename")
	}
	ts, err := time.Parse(`20060102-150405`, match[3])
	if err != nil {
		return ``, ``, 0, errors.New("can't decode timestamp from filename")
	}
	t := ts.Unix()

	return match[1], basePath, t, nil
}

func parseFileList(reader io.Reader, procF func(fileInfo) error) error {
	// File info e.g. generated by:
	// find ${DIR} -type f -printf '%s\t%U\t%i\t%n\t%T@\t%A@\t%C@\t%P\n'
	// Regex to parse file info. We discard fractional part of timestamp.
	re := regexp.MustCompile(`^([0-9]*)\t([0-9]*)\t([0-9]*)\t([0-9]*)\t([0-9]*)(?:\.[0-9]*)\t([0-9]*)(?:\.[0-9]*)\t([0-9]*)(?:\.[0-9]*)\t(.*)$`)

	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		l := scanner.Text()
		m := re.FindStringSubmatch(l)
		if m == nil || len(m) < 9 || m[8] == `` {
			// Parsing line failed
		} else {
			var f fileInfo
			f.size, _ = strconv.ParseUint(m[1], 10, 64)
			uidNum, _ := strconv.ParseUint(m[2], 10, 64)
			f.uid = uidT(uidNum)
			f.mtime, _ = strconv.ParseInt(m[5], 10, 64)
			f.atime, _ = strconv.ParseInt(m[6], 10, 64)
			f.path = m[8]
			procF(f)
		}
	}
	return nil
}

func binTime(t int64, acqTime int64) int {
	age := acqTime - t
	for i, tb := range timeBins {
		if age < int64(tb.maxAgeS) {
			return i
		}
	}
	return len(timeBins) - 1
}

func gatherDirSummary(reader io.Reader, d *dataProvider) error {
	err := parseFileList(reader, func(f fileInfo) error {
		return d.addFileInfo(f)
	})
	return err
}

func sumSz(sz [len(timeBins)]uint64) uint64 {
	sum := uint64(0)
	for i := 0; i < len(timeBins); i++ {
		sum += sz[i]
	}
	return sum
}

func serveDirInfo(w http.ResponseWriter, r *http.Request, d *dataProvider) {
	var dJson dirForJson
	dJson.ComputerName, dJson.BasePath, dJson.AcqTime = d.SourceInfo()

	dJson.Title = `Data age`
	for _, tb := range timeBins {
		dJson.Categories = append(dJson.Categories, tb.txt)
	}

	// Add only legend titles for data that we display
	var legendData legendDataJson
	// FIXME: implement a more reliable way to check if we have access time data
	haveAccessTime := (dJson.ComputerName != ``) || strings.HasPrefix(dJson.BasePath, `\\serine`)
	if haveAccessTime {
		legendData.Name = `Time accessed`
		dJson.LegendData = append(dJson.LegendData, legendData)
	}
	legendData.Name = `Time modified`
	dJson.LegendData = append(dJson.LegendData, legendData)

	enc := json.NewEncoder(w)
	enc.SetIndent(``, `    `) // Pretty print output

	dir := r.FormValue(`dir`)
	if dir == `` {
		dir = `.`
	}
	// Check if we have data for this dir
	if d.dirExists(dir) {
		// Only add access time data if it is valid
		if haveAccessTime {
			for i := 0; i < len(timeBins); i++ {
				dJson.DataAcc = append(dJson.DataAcc, float64(d.dirSizeAccTimeBin(dir, i))/GigaByte)
			}
		}
		for i := 0; i < len(timeBins); i++ {
			dJson.DataMod = append(dJson.DataMod, float64(d.dirSizeModTimeBin(dir, i))/GigaByte)
		}

		// Gather data for pie chart with sub directories
		// Create slice with name and total size of subdirs
		var subDirs []subDir
		sdSumSize := uint64(0)
		for _, sd := range d.subDirs(dir) {
			var sdTot subDir
			sdTot.name = sd
			sdFull := path.Join(dir, sd)
			sdSize := d.subDirSize(sdFull)
			sdSumSize += sdSize
			sdTot.size = sdSize
			subDirs = append(subDirs, sdTot)
		}
		// Sort so that pie chart is in descending size order
		// If size is equal, return in alphabetical name order
		sort.Slice(subDirs, func(i int, j int) bool {
			if subDirs[i].size == subDirs[j].size {
				return subDirs[i].name < subDirs[j].name
			}
			return subDirs[i].size > subDirs[j].size
		})

		combinePieItems := false
		// Check if we must show the content of the previously combined ("other") subdirectories
		other := r.FormValue(`other`)
		if other == `` { // No, display as usual and combine superfluous dirs

			// Only keep top "MaxPieParts" directories, group the rest under "other"
			// Avoid putting a single item in "Other". which happens when there are MaxPieParts+1 items
			combinePieItems = len(subDirs) > MaxPieParts
			if combinePieItems {
				otherSize := uint64(0)
				for i := MaxPieParts; i < len(subDirs); i++ {
					otherSize += subDirs[i].size
				}
				// Replace last item
				otherDirCount := len(subDirs) - MaxPieParts
				subDirs[MaxPieParts].name = `Other (` + strconv.Itoa(otherDirCount) + ` directories)`
				subDirs[MaxPieParts].size = otherSize
				// Truncate slice
				subDirs = subDirs[:MaxPieParts+1]
			}
		} else {
			// Only keep the directories that where previously combined
			subDirs = subDirs[MaxPieParts:]
			// We only want total the size of the remaining dirs, so recompute
			sdSumSize = uint64(0)
			for _, sd := range subDirs {
				sdSumSize += sd.size
			}

		}
		// We need to add unstructured data to the JSON.
		// This is done according to https://blog.golang.org/json
		for i, p := range subDirs {
			PiedataIntf := make(map[string]interface{})
			PiedataIntf[`name`] = p.name
			PiedataIntf[`value`] = float64(p.size) / GigaByte

			// If directories are combined in "Other", display this section in grey
			if combinePieItems && i == len(subDirs)-1 {
				s := make(map[string]interface{})
				s[`color`] = `#aaaaaa`
				PiedataIntf[`itemStyle`] = s
			}

			dJson.PieData = append(dJson.PieData, PiedataIntf)
		}
		if other == `` {
			dJson.PieTitle = `Sub directories`
		} else {
			dJson.PieTitle = `"Other" sub directories`
		}
		dJson.PieSubTitle = fmt.Sprintf(`Total size sub directories: %.3f GB`, float64(sdSumSize)/GigaByte)
	} else {
		log.Printf("No data for dir: %s\n", dir)
	}
	if err := enc.Encode(dJson); err != nil {
		panic(err)
	}
}

func serveDiskUsage(d *dataProvider, serveDir string) {
	var httpFs http.Handler
	if debug {
		httpFs = http.FileServer(http.Dir("web"))
	} else {
		w, _ := fs.Sub(www, "web")
		httpFs = http.FileServer(http.FS(w))
	}
	http.Handle(serveDir+"/", http.StripPrefix(serveDir+"/", httpFs))
	http.HandleFunc(serveDir+"/dirinfo.json", func(w http.ResponseWriter, r *http.Request) {
		serveDirInfo(w, r, d)
	})
	log.Printf("Listening on :3000%s/storageprobe.html\n", serveDir)
	err := http.ListenAndServe(":3000", nil)
	if err != nil {
		log.Fatal(err)
	}
}

var debug bool

func main() {
	var fn string
	var serveDir string
	var d dataProvider

	flag.StringVar(&fn, `filelist`, ``, `File with directory list`)
	flag.StringVar(&serveDir, `servedir`, `/storageprobe`, `Web directory under which pages will be served`)
	flag.BoolVar(&debug, `debug`, false, `Debug mode, webserver serves files from disk instead of build in`)
	flag.Parse()
	computerName, basePath, acqTime, err := decodeFindFilename(fn)
	if err != nil {
		log.Fatalf("Decoding data source from filename %s failed:%s", fn, err.Error())
	}
	d.SetSourceInfo(computerName, basePath, acqTime)
	f, err := os.Open(fn)
	if err != nil {
		log.Fatalf("Open %s failed:%s", fn, err.Error())
	}
	defer f.Close()
	dirSum := make(map[string]dirSummary)
	d.dirSummap = dirSum

	err = gatherDirSummary(f, &d)
	if err != nil {
		log.Fatalf("gatherDirSummary failed:%s", err.Error())
	}
	serveDiskUsage(&d, serveDir)
}
*/
