// Package dbtest gives tests a fresh, fully migrated SQLite database quickly.
//
// Running every migration on an empty file costs about 2 s under -race, and
// the handler tests open hundreds of databases. The migrations run once per
// test binary into a template file; each Open copies that file, and
// database.Open then finds nothing left to migrate.
package dbtest

import (
	"io"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/caioricciuti/ch-ui/internal/database"
)

var (
	templateOnce sync.Once
	templatePath string
	templateErr  error
)

func template() (string, error) {
	templateOnce.Do(func() {
		dir, err := os.MkdirTemp("", "chui-dbtest-")
		if err != nil {
			templateErr = err
			return
		}
		path := filepath.Join(dir, "template.db")
		db, err := database.Open(path)
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

// Open returns a migrated database stored as name in t.TempDir().
func Open(t testing.TB, name string) (*database.DB, error) {
	t.Helper()
	return OpenIn(t, t.TempDir(), name)
}

// OpenIn is Open for tests that need the database at a known path, for
// example as the config's DatabasePath.
func OpenIn(t testing.TB, dir, name string) (*database.DB, error) {
	t.Helper()
	src, err := template()
	if err != nil {
		return nil, err
	}
	dst := filepath.Join(dir, name)
	if err := copyFile(src, dst); err != nil {
		return nil, err
	}
	return database.Open(dst)
}

func copyFile(src, dst string) error {
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
