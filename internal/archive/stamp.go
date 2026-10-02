package archive

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"
)

// Page timestamps. A page carries two stamps, each answering a different "when did
// this change?":
//
//   - ModifiedAt: anything about the page in the archive changed — its metadata,
//     SVG, transcription or tags. Owned by WritePage, the single page-doc writer,
//     which compares the doc it is handed with the stored one.
//   - DeviceModifiedAt: the page changed on the Supernote — new or moved page,
//     handwriting, titles/links/keywords, star, background. Owned by Write (ingest),
//     the only path that sees the device, through DeviceHash.
//
// Both are UTC at second precision, so they serialize as plain RFC3339.

// now is the archive clock: a.Now when set (tests), else the wall clock.
func (a *Archive) now() time.Time {
	now := time.Now
	if a.Now != nil {
		now = a.Now
	}
	return now().UTC().Truncate(time.Second)
}

// sameContent reports whether two page docs differ only in bookkeeping — the
// schema version and the change-tracking fields themselves. Comparing the
// marshalled form keeps the verdict identical to the bytes on disk.
func sameContent(a, b PageDoc) bool {
	strip := func(pd PageDoc) []byte {
		pd.SchemaVersion, pd.DeviceHash = 0, ""
		pd.ModifiedAt, pd.DeviceModifiedAt = time.Time{}, time.Time{}
		b, _ := json.Marshal(pd)
		return b
	}
	return bytes.Equal(strip(a), strip(b))
}

// deviceHash fingerprints what the device decided about a page: its device-derived
// metadata (pageDoc) and its SVG exactly as the renderer produced it, before the
// archive's SVG pipeline. Restyling through config therefore never moves it, while
// any stroke, star, title, keyword, link or background change does.
func deviceHash(pd PageDoc, rawSVG []byte) string {
	pd.SchemaVersion = 0
	b, _ := json.Marshal(pd)
	h := sha256.New()
	h.Write(b)
	h.Write([]byte{'\n'})
	h.Write(rawSVG)
	return hex.EncodeToString(h.Sum(nil))
}

// PageIDTime is the creation time a supernote id embeds: "P"/"F" + YYYYMMDDHHMMSS
// + tail, in the device's local time. An id carrying only a day yields that day at
// midnight; one with fewer than 8 leading digits (or an invalid date) has none.
func PageIDTime(id string) (time.Time, bool) {
	s := id
	if len(s) > 0 && (s[0] == 'P' || s[0] == 'F') {
		s = s[1:]
	}
	n := 0
	for n < len(s) && s[n] >= '0' && s[n] <= '9' {
		n++
	}
	if n >= 14 {
		if t, err := time.ParseInLocation("20060102150405", s[:14], time.Local); err == nil {
			return t, true
		}
	}
	if n >= 8 {
		if t, err := time.ParseInLocation("20060102", s[:8], time.Local); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}
