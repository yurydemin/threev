package filemanager

import (
	"sort"
	"strings"

	"threev/internal/domain"
)

// sortEntries returns a new slice containing entries reordered per FR-FM-004:
// folders always first (sorted among themselves by Key, ascending), followed
// by files sorted by sortBy/sortOrder. It never mutates entries, so callers
// holding a cached, S3-ordered slice (see listCache) can sort a copy of it
// repeatedly (e.g. on every SortBy/SortOrder change) without disturbing the
// cached order.
//
// sortBy is one of "name", "size", "type", "modified"; an empty or
// unrecognized value defaults to "name". sortOrder is "asc" or "desc"; an
// empty or unrecognized value defaults to "asc".
func sortEntries(entries []domain.ObjectEntry, sortBy, sortOrder string) []domain.ObjectEntry {
	sorted := make([]domain.ObjectEntry, len(entries))
	copy(sorted, entries)

	desc := sortOrder == "desc"

	sort.SliceStable(sorted, func(i, j int) bool {
		a, b := sorted[i], sorted[j]

		// Folders always sort before files, regardless of sortBy/sortOrder.
		if a.IsFolder != b.IsFolder {
			return a.IsFolder
		}

		// Folders are always ordered by name, ascending, among themselves.
		if a.IsFolder {
			return a.Key < b.Key
		}

		less := lessFile(a, b, sortBy)
		if desc {
			return !less
		}

		return less
	})

	return sorted
}

// lessFile reports whether file a should sort before file b for the given
// sortBy column, in ascending order. Ties fall back to Key so ordering stays
// deterministic.
func lessFile(a, b domain.ObjectEntry, sortBy string) bool {
	switch sortBy {
	case "size":
		if a.Size != b.Size {
			return a.Size < b.Size
		}
	case "type":
		if a.ContentType != b.ContentType {
			return a.ContentType < b.ContentType
		}
	case "modified":
		if !a.LastModified.Equal(b.LastModified) {
			return a.LastModified.Before(b.LastModified)
		}
	case "name":
		// handled by the Key fallback below
	default:
		// unrecognized sortBy (including "") defaults to name, handled by
		// the Key fallback below
	}

	return strings.Compare(a.Key, b.Key) < 0
}
