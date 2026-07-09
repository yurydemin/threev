package filemanager

import (
	"testing"
	"time"

	"threev/internal/domain"
)

func sortTestEntries() []domain.ObjectEntry {
	t1 := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	t2 := time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)
	t3 := time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC)

	return []domain.ObjectEntry{
		{Key: "zeta/", IsFolder: true},
		{Key: "b.txt", Size: 300, ContentType: "text/plain", LastModified: t2},
		{Key: "alpha/", IsFolder: true},
		{Key: "a.png", Size: 100, ContentType: "image/png", LastModified: t1},
		{Key: "c.json", Size: 200, ContentType: "application/json", LastModified: t3},
	}
}

func keys(entries []domain.ObjectEntry) []string {
	out := make([]string, len(entries))
	for i, e := range entries {
		out[i] = e.Key
	}

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

func TestSortEntriesFoldersAlwaysFirst(t *testing.T) {
	t.Parallel()

	tests := []struct {
		sortBy    string
		sortOrder string
	}{
		{"name", "asc"},
		{"name", "desc"},
		{"size", "asc"},
		{"size", "desc"},
		{"type", "asc"},
		{"type", "desc"},
		{"modified", "asc"},
		{"modified", "desc"},
	}

	for _, tt := range tests {
		t.Run(tt.sortBy+"_"+tt.sortOrder, func(t *testing.T) {
			t.Parallel()

			sorted := sortEntries(sortTestEntries(), tt.sortBy, tt.sortOrder)

			if len(sorted) < 2 {
				t.Fatalf("sorted has %d entries, want at least 2 folders", len(sorted))
			}
			if !sorted[0].IsFolder || !sorted[1].IsFolder {
				t.Fatalf("first two entries = %+v, %+v; want both folders", sorted[0], sorted[1])
			}
			for _, e := range sorted[2:] {
				if e.IsFolder {
					t.Fatalf("found folder %q after files: %v", e.Key, keys(sorted))
				}
			}
			// Folders themselves are always ordered by name ascending,
			// regardless of sortBy/sortOrder.
			if sorted[0].Key != "alpha/" || sorted[1].Key != "zeta/" {
				t.Errorf("folder order = %v, want [alpha/ zeta/]", []string{sorted[0].Key, sorted[1].Key})
			}
		})
	}
}

func TestSortEntriesByName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		sortOrder string
		want      []string
	}{
		{"asc", "asc", []string{"alpha/", "zeta/", "a.png", "b.txt", "c.json"}},
		{"desc", "desc", []string{"alpha/", "zeta/", "c.json", "b.txt", "a.png"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			sorted := sortEntries(sortTestEntries(), "name", tt.sortOrder)
			if got := keys(sorted); !equalStrings(got, tt.want) {
				t.Errorf("keys = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSortEntriesBySize(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		sortOrder string
		want      []string
	}{
		{"asc", "asc", []string{"alpha/", "zeta/", "a.png", "c.json", "b.txt"}},
		{"desc", "desc", []string{"alpha/", "zeta/", "b.txt", "c.json", "a.png"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			sorted := sortEntries(sortTestEntries(), "size", tt.sortOrder)
			if got := keys(sorted); !equalStrings(got, tt.want) {
				t.Errorf("keys = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSortEntriesByType(t *testing.T) {
	t.Parallel()

	// ContentType values: a.png=image/png, b.txt=text/plain,
	// c.json=application/json - ascending alphabetical order is
	// application/json < image/png < text/plain.
	tests := []struct {
		name      string
		sortOrder string
		want      []string
	}{
		{"asc", "asc", []string{"alpha/", "zeta/", "c.json", "a.png", "b.txt"}},
		{"desc", "desc", []string{"alpha/", "zeta/", "b.txt", "a.png", "c.json"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			sorted := sortEntries(sortTestEntries(), "type", tt.sortOrder)
			if got := keys(sorted); !equalStrings(got, tt.want) {
				t.Errorf("keys = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSortEntriesByModified(t *testing.T) {
	t.Parallel()

	// LastModified: a.png=Jan, c.json=Mar, b.txt=Jun.
	tests := []struct {
		name      string
		sortOrder string
		want      []string
	}{
		{"asc", "asc", []string{"alpha/", "zeta/", "a.png", "c.json", "b.txt"}},
		{"desc", "desc", []string{"alpha/", "zeta/", "b.txt", "c.json", "a.png"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			sorted := sortEntries(sortTestEntries(), "modified", tt.sortOrder)
			if got := keys(sorted); !equalStrings(got, tt.want) {
				t.Errorf("keys = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSortEntriesDefaultsToNameAscending(t *testing.T) {
	t.Parallel()

	want := []string{"alpha/", "zeta/", "a.png", "b.txt", "c.json"}

	tests := []struct {
		name      string
		sortBy    string
		sortOrder string
	}{
		{"empty sortBy and sortOrder", "", ""},
		{"unrecognized sortBy", "bogus", "asc"},
		{"unrecognized sortOrder defaults to asc", "name", "bogus"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			sorted := sortEntries(sortTestEntries(), tt.sortBy, tt.sortOrder)
			if got := keys(sorted); !equalStrings(got, want) {
				t.Errorf("keys = %v, want %v", got, want)
			}
		})
	}
}

func TestSortEntriesDoesNotMutateInput(t *testing.T) {
	t.Parallel()

	original := sortTestEntries()
	beforeKeys := keys(original)

	_ = sortEntries(original, "size", "desc")

	afterKeys := keys(original)
	if !equalStrings(beforeKeys, afterKeys) {
		t.Fatalf("input slice order changed: before=%v after=%v", beforeKeys, afterKeys)
	}
}
