package archive

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// stampClock is a settable archive clock: tick moves it a day forward, so every
// stamp a step writes is distinguishable from the one before.
type stampClock struct{ t time.Time }

func (c *stampClock) now() time.Time { return c.t }
func (c *stampClock) tick()          { c.t = c.t.Add(24 * time.Hour) }

func newStamped(t *testing.T) (*Archive, *stampClock) {
	t.Helper()
	c := &stampClock{t: time.Date(2026, time.July, 1, 12, 0, 0, 0, time.UTC)}
	a := New(t.TempDir())
	a.Now = c.now
	return a, c
}

func readStamps(t *testing.T, a *Archive, pageID string) PageDoc {
	t.Helper()
	pd, err := a.ReadPage("F_TEST", pageID)
	if err != nil {
		t.Fatal(err)
	}
	return pd
}

func TestWritePageStampsModified(t *testing.T) {
	a, c := newStamped(t)
	if err := os.MkdirAll(filepath.Join(a.Root, "F_TEST"), 0o755); err != nil {
		t.Fatal(err)
	}
	created := c.t
	if _, err := a.WritePage("F_TEST", PageDoc{PageID: "Pa"}, false); err != nil {
		t.Fatal(err)
	}
	if got := readStamps(t, a, "Pa").ModifiedAt; !got.Equal(created) {
		t.Fatalf("new page ModifiedAt = %v, want %v", got, created)
	}

	c.tick()
	pd := readStamps(t, a, "Pa")
	if changed, err := a.WritePage("F_TEST", pd, false); err != nil || changed {
		t.Fatalf("unchanged write: changed=%v err=%v, want a no-op", changed, err)
	}

	pd.DeviceHash = "baseline"
	if changed, err := a.WritePage("F_TEST", pd, false); err != nil || !changed {
		t.Fatalf("bookkeeping write: changed=%v err=%v, want written", changed, err)
	}
	if got := readStamps(t, a, "Pa").ModifiedAt; !got.Equal(created) {
		t.Errorf("bookkeeping-only change bumped ModifiedAt to %v", got)
	}

	pd = readStamps(t, a, "Pa")
	if _, err := a.WritePage("F_TEST", pd, true); err != nil {
		t.Fatal(err)
	}
	if got := readStamps(t, a, "Pa").ModifiedAt; !got.Equal(c.t) {
		t.Errorf("touched write ModifiedAt = %v, want %v", got, c.t)
	}

	c.tick()
	pd.Starred = true
	if _, err := a.WritePage("F_TEST", pd, false); err != nil {
		t.Fatal(err)
	}
	if got := readStamps(t, a, "Pa").ModifiedAt; !got.Equal(c.t) {
		t.Errorf("content change ModifiedAt = %v, want %v", got, c.t)
	}
}

func TestIngestStamps(t *testing.T) {
	a, c := newStamped(t)
	svgs := svgMap(map[string]string{"Pa": `<svg><path fill="#000000" d="M0 0"/></svg>`})
	write := func(svgs map[string][]byte) {
		t.Helper()
		if _, err := a.Write(note("Pa"), svgs); err != nil {
			t.Fatal(err)
		}
	}
	expect := func(step string, mod, dev time.Time) {
		t.Helper()
		pd := readStamps(t, a, "Pa")
		if !pd.ModifiedAt.Equal(mod) || !pd.DeviceModifiedAt.Equal(dev) {
			t.Errorf("%s: stamps (mod %v, dev %v), want (%v, %v)", step, pd.ModifiedAt, pd.DeviceModifiedAt, mod, dev)
		}
	}

	day1 := c.t
	write(svgs)
	expect("first ingest", day1, day1)

	c.tick()
	write(svgs)
	expect("unchanged re-ingest", day1, day1)

	// Restyling rewrites the SVG but the device saw nothing.
	day2 := c.t
	a.SVG.Colors = map[string]string{"black": "red"}
	write(svgs)
	expect("restyle", day2, day1)

	c.tick()
	day3 := c.t
	write(svgMap(map[string]string{"Pa": "<svg>EDITED</svg>"}))
	expect("handwriting change", day3, day3)
}

func TestIngestStarBumpsDevice(t *testing.T) {
	a, c := newStamped(t)
	svgs := svgMap(map[string]string{"Pa": "<svg>a</svg>"})
	n := note("Pa")
	if _, err := a.Write(n, svgs); err != nil {
		t.Fatal(err)
	}
	c.tick()
	n.Pages[0].Starred = true
	if _, err := a.Write(n, svgs); err != nil {
		t.Fatal(err)
	}
	pd := readStamps(t, a, "Pa")
	if !pd.DeviceModifiedAt.Equal(c.t) || !pd.ModifiedAt.Equal(c.t) {
		t.Errorf("star change stamps (mod %v, dev %v), want both %v", pd.ModifiedAt, pd.DeviceModifiedAt, c.t)
	}
}

// TestMigrateStampsFromPageID: a v5 page gets both stamps from its PAGEID, and the
// first ingest afterwards records the device hash as a baseline without moving
// either stamp.
func TestMigrateStampsFromPageID(t *testing.T) {
	a, c := newStamped(t)
	const id = "P20260415093000AB"
	svgs := svgMap(map[string]string{id: "<svg>a</svg>"})
	if _, err := a.Write(note(id), svgs); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(a.Root, "F_TEST", id+".json")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"modified_at", "device_modified_at", "device_hash"} {
		delete(m, k)
	}
	m["schema_version"] = 5
	if b, err = json.Marshal(m); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := a.MigrateAll(context.Background(), MigrateOptions{}); err != nil {
		t.Fatal(err)
	}
	created := time.Date(2026, time.April, 15, 9, 30, 0, 0, time.Local).UTC()
	pd := readStamps(t, a, id)
	if !pd.ModifiedAt.Equal(created) || !pd.DeviceModifiedAt.Equal(created) || pd.DeviceHash != "" {
		t.Fatalf("migrated stamps (mod %v, dev %v, hash %q), want both %v and no hash",
			pd.ModifiedAt, pd.DeviceModifiedAt, pd.DeviceHash, created)
	}

	c.tick()
	if _, err := a.Write(note(id), svgs); err != nil {
		t.Fatal(err)
	}
	pd = readStamps(t, a, id)
	if !pd.ModifiedAt.Equal(created) || !pd.DeviceModifiedAt.Equal(created) || pd.DeviceHash == "" {
		t.Errorf("post-migrate ingest stamps (mod %v, dev %v, hash %q), want both %v and a hash",
			pd.ModifiedAt, pd.DeviceModifiedAt, pd.DeviceHash, created)
	}
}

func TestPageIDTime(t *testing.T) {
	for _, tc := range []struct {
		id   string
		want time.Time
		ok   bool
	}{
		{"P20260415093000AB", time.Date(2026, time.April, 15, 9, 30, 0, 0, time.Local), true},
		{"F20260415093000", time.Date(2026, time.April, 15, 9, 30, 0, 0, time.Local), true},
		{"P20260415", time.Date(2026, time.April, 15, 0, 0, 0, 0, time.Local), true},
		{"P20261399999999", time.Time{}, false},
		{"Pnodate", time.Time{}, false},
	} {
		got, ok := PageIDTime(tc.id)
		if ok != tc.ok || !got.Equal(tc.want) {
			t.Errorf("PageIDTime(%q) = (%v, %v), want (%v, %v)", tc.id, got, ok, tc.want, tc.ok)
		}
	}
}
