package diag

import (
	"fmt"
	"io"

	"github.com/matjam/plutonium/internal/token"
)

// Diag is a diagnostic reporter that writes errors and warnings to a writer.
type Diag struct {
	fset   *token.FileSet
	w      io.Writer
	errors int
	warns  int
}

// New creates a new Diag that writes to w.
func New(fset *token.FileSet, w io.Writer) *Diag {
	return &Diag{fset: fset, w: w}
}

// Errorf reports an error at pos with the given format.
func (d *Diag) Errorf(pos token.Pos, format string, args ...interface{}) {
	d.errors++
	msg := fmt.Sprintf(format, args...)
	if pos.IsValid() {
		p := d.fset.Position(pos)
		_, _ = fmt.Fprintf(d.w, "%s: error: %s\n", p, msg)
	} else {
		_, _ = fmt.Fprintf(d.w, "error: %s\n", msg)
	}
}

// Warnf reports a warning at pos with the given format.
func (d *Diag) Warnf(pos token.Pos, format string, args ...interface{}) {
	d.warns++
	msg := fmt.Sprintf(format, args...)
	if pos.IsValid() {
		p := d.fset.Position(pos)
		_, _ = fmt.Fprintf(d.w, "%s: warning: %s\n", p, msg)
	} else {
		_, _ = fmt.Fprintf(d.w, "warning: %s\n", msg)
	}
}

func (d *Diag) ErrorCount() int { return d.errors }
func (d *Diag) WarnCount() int  { return d.warns }
func (d *Diag) HasErrors() bool { return d.errors > 0 }
