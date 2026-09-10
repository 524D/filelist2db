#!/bin/bash
echo "Deleting old database files"
rm -f db.sqlite db.sqlite-journal
echo "compiling filelist2db.exe"
go build
echo "running filelist2db.exe"
./filelist2db.exe -cpuprofile cpuprofile.prof --build-dir-summary testdata/_mycomputer_E%3Adummydir1%5Cdummydir2%5C_20220301-134000.lst
# ./filelist2db.exe -cpuprofile cpuprofile2.prof -db db2.sqlite -build-dir-summary /c/Users/rjmarissen/data/filelists/%5C%5Ccpm-archive%5Ccpm-archive_20250829-172723.lst
go tool pprof -http=:8080 cpuprofile.prof
# Open the browser and go to http://localhost:8080/ui/ to view the profile
bash -c "sleep 2; start chrome \"http://localhost:8080/ui/\"" &