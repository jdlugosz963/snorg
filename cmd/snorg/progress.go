package main

// Progress reporting for the batch commands (ingest, analyze, migrate). Every byte
// written here goes to stderr, so stdout stays the machine-readable channel —
// query/list/retrieve/export are untouched, and `snorg analyze … 2>/dev/null`
// silences progress without losing anything else.
//
// Three modes, chosen once at construction:
//
//	bar      not -v, stderr is a terminal — one animated line, rewritten in place
//	plain    not -v, stderr is not a terminal — one line per item, no ANSI
//	verbose  -v — one line per item plus indented detail, no bar
//
// Hand-rolled on purpose: no isatty, no x/term, no progress library (see the
// project's dependency rule).

import (
	"fmt"
	"io"
	"os"
	"strings"
)

type reportMode int

const (
	modeBar reportMode = iota
	modePlain
	modeVerbose
)

// barWidth is fixed because without x/term there is no way to ask the terminal how
// wide it is, and guessing wrong wraps the line and defeats the redraw.
const barWidth = 24

// reporter renders a batch's per-item progress. Every batch command drives it
// sequentially, one item at a time, so a bar redraw can never interleave with a
// failure line.
type reporter struct {
	w     io.Writer
	mode  reportMode
	label string // the bar's verb: "analyze", "ingest", "migrate"
	unit  string // counter-mode noun when the total is unknown: "files"
	total int    // 0 = unknown: render a counter instead of a bar

	done  int
	last  string // most recently finished item, shown on the bar
	drawn bool   // a bar line is on screen and must be erased before other output
}

// newReporter picks the mode from verbose and whether w is a terminal. A total <= 0
// selects the counter form, for a batch whose size is not knowable up front
// (migrate: one page can yield both a JSON result and a .md.diff result, so any
// precomputed denominator would be a lower bound and the bar would run past 100%).
func newReporter(w io.Writer, total int, label, unit string, verbose bool) *reporter {
	mode := modeBar
	switch {
	case verbose:
		mode = modeVerbose
	case !isTerminal(w):
		mode = modePlain
	}
	return &reporter{w: w, mode: mode, label: label, unit: unit, total: total}
}

// tick records one finished item that is not worth a line of its own — an
// already-current file in a migrate run, say. It is still progress, so it advances
// the bar; it is just not news, so plain and verbose modes stay quiet.
func (r *reporter) tick(id string) {
	r.done++
	r.last = id
	if r.mode == modeBar {
		r.draw()
	}
}

// item records one finished item: summary is the one-line "what happened", detail
// the verbose-only extra lines, printed indented in order.
func (r *reporter) item(id, summary string, detail []string) {
	r.done++
	r.last = id
	switch r.mode {
	case modeBar:
		r.draw()
	case modePlain:
		fmt.Fprintf(r.w, "%s: %s\n", id, summary)
	case modeVerbose:
		fmt.Fprintf(r.w, "%s: %s\n", id, summary)
		for _, d := range detail {
			fmt.Fprintf(r.w, "  %s\n", d)
		}
	}
}

// fail records one failed item. It prints in every mode — a failure is never
// swallowed by the bar.
func (r *reporter) fail(id string, err error) {
	r.done++
	r.last = id
	r.erase()
	fmt.Fprintf(r.w, "failed %s: %v\n", id, err)
	if r.mode == modeBar {
		r.draw()
	}
}

// finish erases any bar and prints the batch's closing summary.
func (r *reporter) finish(summary string) {
	r.erase()
	fmt.Fprintln(r.w, summary)
}

// close erases any bar without printing. Every action defers it, so an early error
// return never leaves a half-drawn bar on the terminal.
func (r *reporter) close() {
	r.erase()
}

// erase clears a drawn bar line.
func (r *reporter) erase() {
	if r.drawn {
		fmt.Fprint(r.w, "\r\x1b[K")
		r.drawn = false
	}
}

// draw renders the bar (or the counter, when the total is unknown) in place. The
// caller.
func (r *reporter) draw() {
	if r.total > 0 {
		filled := barWidth * r.done / r.total
		if filled > barWidth {
			filled = barWidth
		}
		fmt.Fprintf(r.w, "\r%s [%s%s] %d/%d %s\x1b[K", r.label,
			strings.Repeat("#", filled), strings.Repeat("-", barWidth-filled),
			r.done, r.total, r.last)
	} else {
		fmt.Fprintf(r.w, "\r%s %d %s  %s\x1b[K", r.label, r.done, r.unit, r.last)
	}
	r.drawn = true
}

// isTerminal reports whether w is a terminal that can carry an animated line: the
// same os.Stat/ModeCharDevice idiom as stdinPiped, plus a TERM check so a dumb
// terminal (or none) gets plain lines instead of escape sequences. A non-*os.File
// writer — a test buffer — is never a terminal.
func isTerminal(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	fi, err := f.Stat()
	if err != nil || fi.Mode()&os.ModeCharDevice == 0 {
		return false
	}
	switch os.Getenv("TERM") {
	case "", "dumb":
		return false
	}
	return true
}
