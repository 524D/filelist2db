package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"time"

	"github.com/524D/filelist2db/dataprovider"
	"github.com/524D/filelist2db/dataprovidersqlite"
	"github.com/schollz/progressbar/v3"
	_ "modernc.org/sqlite"
)

type Args struct {
	dbFile          string
	buildDirSummary bool
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

var (
	fileListLineRE = regexp.MustCompile(`^([0-9]*)\t([0-9]*)\t([0-9]*)\t([0-9]*)\t([0-9]*)(?:\.[0-9]*)\t([0-9]*)(?:\.[0-9]*)\t([0-9]*)(?:\.[0-9]*)\t(.*)$`)
	findFilenameRE = regexp.MustCompile(`^(?:.*[/\\])?(?:_([^/\\]*?)_)?([^/\\]*?)_([0-9]{8}-[0-9]{6})\.(?:lst|txt)$`)
)

func parseCmdLine() []string {
	// Parse command line arguments
	// -db <database>: name of sqlite database to use (default: "db.sqlite")
	// Remaining arguments: file lists to process

	// If no arguments, print usage and exit
	// If -h or --help, print usage and exit

	flag.Usage = func() {
		w := flag.CommandLine.Output()

		fmt.Fprintf(w, "Usage: %s <flags> <file1> [<file2> ...]\n", os.Args[0])
		fmt.Fprintln(w, "Where <filex> is a list of files with metadata.")
		fmt.Fprintln(w, "This list is generally created with a Unix 'find' command:")
		fmt.Fprintf(w, "  find ${DIR} -type f -printf '%%s\\t%%U\\t%%i\\t%%n\\t%%T@\\t%%A@\\t%%C@\\t%%P\\n'\n")
		fmt.Fprintln(w, "Flags:")
		flag.PrintDefaults()
	}

	flag.StringVar(&args.dbFile, "db", "db.sqlite", "name of sqlite database to use")
	flag.BoolVar(&args.buildDirSummary, "build-dir-summary", true, "rebuild the dir summary table from file2 after processing input files")
	flag.Parse()

	files := flag.Args()
	// If no files are provided, only allow a summary-only run when enabled.
	if len(files) == 0 && !args.buildDirSummary {
		flag.Usage()
	}

	// For each file, check common errors
	for _, fn := range files {
		inf, err := os.Stat(fn)
		if err != nil {
			fmt.Fprintln(os.Stdout, "File ", fn, " error: ", err)
			flag.Usage()
		}
		if inf.IsDir() {
			fmt.Fprintln(os.Stdout, "File is a directory: ", fn)
			flag.Usage()
		}
		if inf.Size() == 0 {
			fmt.Fprintln(os.Stdout, "File is empty: ", fn)
			flag.Usage()
		}
	}

	return files
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
	match := findFilenameRE.FindStringSubmatch(fn)
	if len(match) < 4 {
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

func parseFileInfoLine(line string) (dataprovider.FileInfo, bool) {
	m := fileListLineRE.FindStringSubmatch(line)
	if len(m) < 9 || m[8] == `` {
		return dataprovider.FileInfo{}, false
	}

	var f dataprovider.FileInfo
	f.Size, _ = strconv.ParseUint(m[1], 10, 64)
	uidNum, _ := strconv.ParseUint(m[2], 10, 64)
	f.Uid = dataprovider.UidT(uidNum)
	f.Mtime, _ = strconv.ParseInt(m[5], 10, 64)
	f.Atime, _ = strconv.ParseInt(m[6], 10, 64)
	f.Path = m[8]
	return f, true
}

func parseFileList(d dataprovider.DataProvider, reader io.ReadSeeker, progress dataprovider.ProgressFunc) error {
	// parseFileList parses a file list and adds the files to the data provider.
	// File info e.g. generated by:
	// find ${DIR} -type f -printf '%s\t%U\t%i\t%n\t%T@\t%A@\t%C@\t%P\n'

	// Get the length of the file to provide a progress indicator
	fileSize, err := reader.Seek(0, io.SeekEnd)
	if err != nil {
		return err
	}
	_, err = reader.Seek(0, io.SeekStart)
	if err != nil {
		return err
	}
	prevPct := int64(-1)

	// Read file line by line
	scanner := bufio.NewScanner(reader)
	if err := d.StartTransaction(); err != nil {
		return err
	}
	lineCount := 0
	for scanner.Scan() {
		f, ok := parseFileInfoLine(scanner.Text())
		if ok {
			if err := d.AddFile(f); err != nil {
				return err
			}
		}

		// FIXME: reduce memory usage at the cost of robustness
		// Commit transaction periodically to avoid memory issues with large datasets
		if lineCount > 0 && lineCount%10000 == 0 {
			if err := d.CommitTransaction(); err != nil {
				return err
			}
			if err := d.StartTransaction(); err != nil {
				return err
			}
		}

		if lineCount%1000 == 0 {
			filePos, err := reader.Seek(0, io.SeekCurrent)
			if err != nil {
				return err
			}
			pct := filePos * 100 / fileSize
			if pct != prevPct && progress != nil {
				progress(filePos, fileSize)
				prevPct = pct
			}
		}
		lineCount++
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	return d.CommitTransaction()
}

func processListFile(d dataprovider.DataProvider, fn string) error {
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

	bar := progressbar.NewOptions(100, progressbar.OptionSetDescription("Processing file list: "+fn))
	bar.Set64(0)
	progress := func(current, total int64) {
		if total <= 0 {
			return
		}
		pct := current * 100 / total
		bar.Set64(pct)
	}
	return parseFileList(d, f, progress)
}

func main() {
	// Parse command line arguments
	files := parseCmdLine()

	// Create data provider
	d, err := dataprovidersqlite.InitDataProviderSqlite(args.dbFile)
	if err != nil {
		panic(err)
	}
	defer d.Finalize()

	// Process files
	for _, fn := range files {
		err = processListFile(d, fn)
		if err != nil {
			panic(err)
		}
	}

	if args.buildDirSummary {
		fmt.Println()
		bar := progressbar.NewOptions(100, progressbar.OptionSetDescription("Rebuilding directory summary"))
		bar.Set64(0)
		progress := func(current, total int64) {
			if total <= 0 {
				return
			}
			pct := current * 100 / total
			bar.Set64(pct)
		}
		err = d.RebuildDirTable(100000, progress)
		if err != nil {
			panic(err)
		}
	}
}
