# Filelist2DB: read a filelist, parse it, and write the results to a database

Example usage:

- Build the database and rebuild the directory summary table after import:
  - `go run . -db db.sqlite filelist1.lst filelist2.lst`
- Skip the directory summary rebuild:
  - `go run . -db db.sqlite -build-dir-summary=false filelist1.lst`
- Rebuild the directory summary without importing any files:
  - `go run . -db db.sqlite -build-dir-summary=true`

The `-build-dir-summary` flag defaults to `true`.

