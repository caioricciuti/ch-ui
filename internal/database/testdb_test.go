package database

import (
	"io"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// Running every migration on an empty file costs about 2 s under -race. The
// migrations run once per test binary into a template file, and each test
// database is a copy of it, so Open finds nothing left to migrate. This is
// internal/database/dbtest for tests inside this package, which cannot import
// it. Tests about migrations themselves still call Open on an empty file.
var (
	templateOnce sync.Once
	templatePath string
	templateErr  error
)

func migratedTemplate() (string, error) {
	templateOnce.Do(func() {
		dir, err := os.MkdirTemp("", "chui-database-test-")
		if err != nil {
			templateErr = err
			return
		}
		path := filepath.Join(dir, "template.db")
		db, err := Open(path)
		if err != nil {
			templateErr = err
			return
		}
		// Closing the last connection checkpoints the WAL into the main
		// file, so the file alone is a complete copy.
		templateErr = db.Close()
		templatePath = path
	})
	return templatePath, templateErr
}

// openMigrated returns a migrated database stored as name in t.TempDir().
func openMigrated(t *testing.T, name string) *DB {
	t.Helper()
	src, err := migratedTemplate()
	if err != nil {
		t.Fatalf("migrate template db: %v", err)
	}
	dst := filepath.Join(t.TempDir(), name)
	if err := copyTestFile(src, dst); err != nil {
		t.Fatalf("copy template db: %v", err)
	}
	db, err := Open(dst)
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func copyTestFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
