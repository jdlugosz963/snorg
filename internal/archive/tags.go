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
	if err := a.WritePage(fileID, pd); err != nil {
		return false, err
	}
	return true, nil
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
