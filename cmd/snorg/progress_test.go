package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

// A non-*os.File writer is never a terminal, so a test buffer always lands in
// plain (or verbose) mode unless the mode is forced.
func TestReporterPlainLines(t *testing.T) {
	var buf bytes.Buffer
	r := newReporter(&buf, 3, "analyze", "pages", false)
	r.item("P1", "new +2/-0", []string{"content: new +2/-0"})
	r.fail("P2", errors.New("page not found"))
	r.item("P3", "skipped", nil)
	r.finish("analyzed 2, skipped 1, conflicted 0, failed 1 of 3 page(s)")

	want := "P1: new +2/-0\n" +
		"failed P2: page not found\n" +
		"P3: skipped\n" +
		"analyzed 2, skipped 1, conflicted 0, failed 1 of 3 page(s)\n"
	if buf.String() != want {
		t.Errorf("plain output =\n%q\nwant\n%q", buf.String(), want)
	}
}

// Verbose keeps the per-item line and adds the detail under it, indented — and
// still draws no bar, so the account scrolls rather than being overwritten.
func TestReporterVerboseDetail(t *testing.T) {
	var buf bytes.Buffer
	r := newReporter(&buf, 1, "analyze", "pages", true)
	r.item("P1", "updated +1/-1, 1 name", []string{"content: updated +1/-1", `title 1: "Esay" -> "Essay"`})

	want := "P1: updated +1/-1, 1 name\n" +
		"  content: updated +1/-1\n" +
		"  title 1: \"Esay\" -> \"Essay\"\n"
	if buf.String() != want {
		t.Errorf("verbose output =\n%q\nwant\n%q", buf.String(), want)
	}
	if strings.Contains(buf.String(), "\x1b") {
		t.Error("verbose mode emitted an escape sequence")
	}
}

// The bar redraws in place and, crucially, is erased before a failure line so the
// two never share a row.
func TestReporterBarRedrawsAroundFailures(t *testing.T) {
	var buf bytes.Buffer
	r := &reporter{w: &buf, mode: modeBar, label: "analyze", total: 10}
	r.item("P1", "new", nil)
	r.fail("P2", errors.New("boom"))
	r.finish("done")

	out := buf.String()
	if !strings.Contains(out, "analyze [") || !strings.Contains(out, "1/10") {
		t.Errorf("no bar drawn: %q", out)
	}
	if !strings.Contains(out, "\r\x1b[K") {
		t.Errorf("bar never erased: %q", out)
	}
	// The failure line must start at column zero, i.e. an erase precedes it.
	if !strings.Contains(out, "\r\x1b[Kfailed P2: boom\n") {
		t.Errorf("failure line not preceded by an erase: %q", out)
	}
	// The closing summary is likewise on its own row.
	if !strings.HasSuffix(out, "\r\x1b[Kdone\n") {
		t.Errorf("summary not preceded by an erase: %q", out)
	}
}

// The bar never exceeds its width even if a batch reports more items than the
// total promised — migrate's counter form exists for that case, but a bar must not
// misrender if one is used anyway.
func TestReporterBarClampsOverflow(t *testing.T) {
	var buf bytes.Buffer
	r := &reporter{w: &buf, mode: modeBar, label: "analyze", total: 1}
	r.item("P1", "new", nil)
	r.item("P2", "new", nil)
	if strings.Contains(buf.String(), strings.Repeat("#", barWidth+1)) {
		t.Errorf("bar overflowed its width: %q", buf.String())
	}
}

// tick is progress without news: it moves the bar but prints nothing, so a run of
// already-current files still shows the batch advancing on a terminal and stays
// silent in a pipe.
func TestReporterTickIsSilentButCounts(t *testing.T) {
	var plain, bar bytes.Buffer
	p := newReporter(&plain, 2, "migrate", "files", false)
	p.tick("note F1")
	p.tick("page P1")
	p.finish("migrated 0, current 2")
	if plain.String() != "migrated 0, current 2\n" {
		t.Errorf("plain tick output = %q, want only the summary", plain.String())
	}

	b := &reporter{w: &bar, mode: modeBar, label: "migrate", unit: "files"}
	b.tick("note F1")
	b.tick("page P1")
	if !strings.Contains(bar.String(), "migrate 2 files") {
		t.Errorf("bar tick output = %q, want the counter to reach 2", bar.String())
	}
}

// A batch whose size is not knowable up front (migrate) counts instead of filling.
func TestReporterCounterMode(t *testing.T) {
	var buf bytes.Buffer
	r := &reporter{w: &buf, mode: modeBar, label: "migrate", unit: "files", total: 0}
	r.item("page Pa", "migrated", nil)
	r.item("diff Pa", "migrated", nil)

	out := buf.String()
	if !strings.Contains(out, "migrate 2 files") {
		t.Errorf("counter =\n%q\nwant a running file count", out)
	}
	// The bar's bracket, not the ANSI erase sequence that ends every redraw.
	if strings.Contains(out, "migrate [") {
		t.Errorf("counter mode drew a bar: %q", out)
	}
}
