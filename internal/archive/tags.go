package archive

import "sort"

// TagPage adds or removes a snorg-managed tag on a single page, keeping the page's
// Tags sorted and de-duplicated. It resolves the owning note (FindPage), reads the
// page doc, mutates its Tags, and writes back only when the set actually changed.
// The bool reports whether the archive was modified (a no-op add/remove writes
// nothing and returns false).
func (a *Archive) TagPage(pageID, tag string, remove bool) (bool, error) {
	fileID, err := a.FindPage(pageID)
	if err != nil {
		return false, err
	}
	pd, err := a.ReadPage(fileID, pageID)
	if err != nil {
		return false, err
	}
	next := applyTag(pd.Tags, tag, remove)
	if equalStrings(pd.Tags, next) {
		return false, nil
	}
	pd.Tags = next
	if _, err := a.WritePage(fileID, pd); err != nil {
		return false, err
	}
	return true, nil
}

// TagNote adds or removes a snorg-managed tag on a whole note, keeping the note's
// Tags sorted and de-duplicated. The tag is stored in note.json only — every page of
// the note inherits it at read time (EffectiveTags), so no <PAGEID>.json is touched.
// The bool reports whether the archive was modified (a no-op add/remove writes
// nothing and returns false).
func (a *Archive) TagNote(fileID, tag string, remove bool) (bool, error) {
	nd, err := a.ReadNote(fileID)
	if err != nil {
		return false, err
	}
	next := applyTag(nd.Tags, tag, remove)
	if equalStrings(nd.Tags, next) {
		return false, nil
	}
	nd.Tags = next
	if err := a.WriteNote(nd); err != nil {
		return false, err
	}
	return true, nil
}

// EffectiveTags is the tag inheritance rule: the sorted, de-duplicated union of the
// note's tags and the page's own (nil when both are empty). It is the single place
// the union is computed — every read surface (query, inventory, retrieve and through
// it serve/export) goes through it, so an inherited tag is indistinguishable from a
// page's own downstream.
func EffectiveTags(nd NoteDoc, pd PageDoc) []string {
	if len(nd.Tags) == 0 {
		return pd.Tags
	}
	if len(pd.Tags) == 0 {
		return nd.Tags
	}
	set := make(map[string]struct{}, len(nd.Tags)+len(pd.Tags))
	for _, t := range nd.Tags {
		set[t] = struct{}{}
	}
	for _, t := range pd.Tags {
		set[t] = struct{}{}
	}
	out := make([]string, 0, len(set))
	for t := range set {
		out = append(out, t)
	}
	sort.Strings(out)
	return out
}

// applyTag returns tags with tag added (remove=false) or removed (remove=true),
// sorted and de-duplicated.
func applyTag(tags []string, tag string, remove bool) []string {
	set := make(map[string]struct{}, len(tags)+1)
	for _, t := range tags {
		set[t] = struct{}{}
	}
	if remove {
		delete(set, tag)
	} else {
		set[tag] = struct{}{}
	}
	if len(set) == 0 {
		return nil
	}
	out := make([]string, 0, len(set))
	for t := range set {
		out = append(out, t)
	}
	sort.Strings(out)
	return out
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
