package dbcommon

import (
	"database/sql"
	"path/filepath"

	_ "modernc.org/sqlite"
)

func OpenDatabase(dbFile string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(dbFile))
	if err != nil {
		return nil, err
	}
	return db, nil
}

func OpenReadOnlyDatabase(dbFile string) (*sql.DB, error) {
	dsn := "file:" + filepath.ToSlash(dbFile) + "?_mode=ro"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	return db, nil
}

func CreateTables(db *sql.DB) error {
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
		node_type INTEGER
	)`)
	if err != nil {
		return err
	}

	_, err = db.Exec(`CREATE INDEX IF NOT EXISTS path_elem_par_idx ON path (path_elem_id, parent_id)`)
	if err != nil {
		return err
	}

	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS file (
		id INTEGER PRIMARY KEY,
		path_id INTEGER,
		size INTEGER,
		mtime INTEGER,
		atime INTEGER,
		uid INTEGER
	)`)
	if err != nil {
		return err
	}

	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS simple_path_elem (
		id INTEGER PRIMARY KEY,
		simple_elem TEXT UNIQUE
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
		simple_path_elem_id INTEGER,
		path_elem_id INTEGER
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

	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS input_file (
		id INTEGER PRIMARY KEY,
		timestamp INTEGER NOT NULL DEFAULT 0
	)`)
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
	_, err = db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS dir_path_unique_idx ON dir (path_id)`)
	if err != nil {
		return err
	}

	return nil
}
