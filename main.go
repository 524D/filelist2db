package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/url"
	"os"
	"regexp"
	"runtime/pprof"
	"strconv"
	"strings"
	"time"

	"github.com/524D/filelist2db/datafiller"
	"github.com/524D/filelist2db/datafillersqlite"
	"github.com/524D/filelist2db/dataprovider"
	"github.com/schollz/progressbar/v3"
	_ "modernc.org/sqlite"
)

type Args struct {
	dbFile          string
	buildDirSummary bool
	cpuProfile      string
}

var args Args

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
		fmt.Fprintln(w, "To visualize a CPU profile: go tool pprof -http=:8080 <profile-file>")
		fmt.Fprintln(w, "Flags:")
		flag.PrintDefaults()
	}

	flag.StringVar(&args.dbFile, "db", "db.sqlite", "name of sqlite database to use")
	flag.BoolVar(&args.buildDirSummary, "build-dir-summary", true, "rebuild the dir summary table from file2 after processing input files")
	flag.StringVar(&args.cpuProfile, "cpuprofile", "", "write a CPU profile to this file; visualize with: go tool pprof -http=:8080 <profile-file>")
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
			log.Fatal("Error accessing file: ", fn, ": ", err)
		}
		if inf.IsDir() {
			log.Fatal("File is a directory: ", fn)
		}
		if inf.Size() == 0 {
			log.Fatal("File is empty: ", fn)
		}
	}

	return files
}

// Decode computer name, base path and search timestamp from filename of "find" result
// The filename should be named like:
// [_computername_][network_share]<_escaped_base_path>_<timestamp>.<txt|lst>
// Where:
//   - computername is the name of the computer whose files are indexed.
//   - if the computername is absent, the network share is used as the computer name.
//     It is an error if both computername and network share are absent.
//   - escaped_base_path is the path under which the file info is obtained
//   - timestamp is the timestamp of the scan, in the format YYYYMMDD-HHMMSS
//
// Since the path name contains characters that cannot be part of a filename,
// it is escaped:
// A leading underscore ("_") is replaced by "%5F"
// Other invalid filename characters are replaced by "%" followed by their ASCII/UTF8
// hex character code, e.g. "%2F" for slash ("/") and "%5C" for backslash ("\").
// "%" is replaced by %25
// The return value is the datasource (the computer name, or if computername is absent and the
// path starts with a network share prefix, the share name), the unescaped base path, and the timestamp as a Unix timestamp (seconds since epoch).
// All path names are normalized to use forward slashes ("/") as path separators, and the base path is stripped of leading and trailing slashes.
//
//	the base path, and the timestamp as a Unix timestamp (seconds since epoch).
func decodeFindFilename(fn string) (string, string, int64, error) {
	match := findFilenameRE.FindStringSubmatch(fn)
	if len(match) < 4 {
		return ``, ``, 0, errors.New("can't extract basepath/timestamp from filename")
	}
	basePath, err := url.QueryUnescape(match[2])
	if err != nil {
		return ``, ``, 0, errors.New("can't decode basepath from filename")
	}
	basePath = strings.ReplaceAll(basePath, `\`, "/")
	dataSource := match[1]
	if dataSource == `` {
		// If computer name is not provided, use the network share as the computer name
		if len(basePath) > 2 && (basePath[0:2] == `\\` || basePath[0:2] == `//`) {
			slashIndex := 2
			// Find the second slash after the network share prefix, or the end of the string if there is no second slash.
			//  The network share is the leading part of the path.
			for range 2 {
				for slashIndex < len(basePath) && basePath[slashIndex] != '/' && basePath[slashIndex] != '\\' {
					slashIndex++
				}
				if slashIndex < len(basePath) {
					slashIndex++ // Move to the character after the slash
				}
			}
			dataSource = basePath[:slashIndex]
			if slashIndex < len(basePath) {
				basePath = basePath[slashIndex+1:]
			} else {
				basePath = ``
			}
		}
	}
	// Strip leading and trailing slashes from basePath
	basePath = strings.TrimLeft(basePath, "/")
	basePath = strings.TrimRight(basePath, "/")
	ts, err := time.Parse(`20060102-150405`, match[3])
	if err != nil {
		return ``, ``, 0, errors.New("can't decode timestamp from filename")
	}
	t := ts.Unix()

	return dataSource, basePath, t, nil
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

func parseFileList(d datafiller.DataFiller, reader io.ReadSeeker, progress dataprovider.ProgressFunc) error {
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
	progress(fileSize, fileSize)
	return d.CommitTransaction()
}

func processListFile(d datafiller.DataFiller, fn string) error {
	dataSource, basePath, acqTime, err := decodeFindFilename(fn)
	if err != nil {
		return err
	}
	d.SetSourceInfo(dataSource, basePath, acqTime)
	f, err := os.Open(fn)
	if err != nil {
		return err
	}
	defer f.Close()

	fmt.Println("Processing file list: " + fn)
	bar := progressbar.NewOptions(100, progressbar.OptionSetDescription("Processing file list"))
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

	if args.cpuProfile != "" {
		f, err := os.Create(args.cpuProfile)
		if err != nil {
			panic(err)
		}
		if err := pprof.StartCPUProfile(f); err != nil {
			panic(err)
		}
		defer pprof.StopCPUProfile()
	}

	// Create the write-side SQLite adapter used to import file metadata.
	f, err := datafillersqlite.InitDataFillerSqlite(args.dbFile)
	if err != nil {
		panic(err)
	}
	defer f.Finalize()

	// Process files
	for _, fn := range files {
		err = processListFile(f, fn)
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
		err = f.RebuildDirTable(10000, progress)
		if err != nil {
			panic(err)
		}
	}
}
