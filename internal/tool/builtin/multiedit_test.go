package builtin

import (
	"errors"
	"io/fs"
	"testing"
)

// TestMultiEditLoadFile_ReadError_Unwraps pins that a read failure loadFile
// can't attribute to fs.ErrNotExist (GAP-4.2) surfaces the underlying error
// via errors.As, not just its string form: loadFile used to build that error
// with %v instead of %w, so a caller doing errors.Is/errors.As up this path
// silently failed here while every sibling error path in the package already
// wrapped correctly.
func TestMultiEditLoadFile_ReadError_Unwraps(t *testing.T) {
	dir := t.TempDir()
	tl := &multieditTool{root: dir}

	// A directory can't be read as a file: os.ReadFile fails with a
	// *fs.PathError distinct from fs.ErrNotExist, landing in loadFile's
	// default branch.
	_, err := tl.loadFile(dir, editSpec{Path: "."})
	if err == nil {
		t.Fatal("expected an error reading a directory as a file")
	}
	var pathErr *fs.PathError
	if !errors.As(err, &pathErr) {
		t.Fatalf("expected wrapped *fs.PathError (via %%w), got %v", err)
	}
}
