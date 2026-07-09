package filemanager

import (
	"sync"

	"threev/internal/domain"
)

// cacheKey identifies one cached listing: a specific bucket prefix ("folder")
// browsed under a specific profile.
type cacheKey struct {
	ProfileID int64
	Bucket    string
	Prefix    string
}

// cacheEntry holds everything ListObjects needs to serve a request straight
// from memory: the raw (unsorted, S3-order) entries accumulated so far
// across however many pages have been fetched, and the S3 pagination state
// from the most recently fetched page.
type cacheEntry struct {
	// Entries is the raw, unsorted accumulation of every page fetched so
	// far for this key. Sorting (sortEntries) is applied by the caller on
	// a copy and never mutates this slice, so the cache always reflects
	// S3's own return order.
	Entries []domain.ObjectEntry
	// NextToken is the ContinuationToken to pass to S3 to fetch the page
	// after Entries; empty when there is none.
	NextToken string
	// IsTruncated mirrors the most recent ListObjectsV2 response: whether
	// S3 has more results beyond Entries.
	IsTruncated bool
}

// listCache is an in-memory, session-lifetime cache of raw S3 listings,
// keyed by profile+bucket+prefix (FR-FM-004/005). It is a deliberately
// simple MVP cache for Stage 2: unbounded, with no eviction/LRU policy -
// acceptable for this stage's expected volume, flagged as tech debt for a
// later stage (see plan's "known risks" section).
//
// It is safe for concurrent use.
type listCache struct {
	mu      sync.Mutex
	entries map[cacheKey]cacheEntry
}

// newListCache returns an empty listCache.
func newListCache() *listCache {
	return &listCache{entries: make(map[cacheKey]cacheEntry)}
}

// get returns the cached entry for key, if any.
func (c *listCache) get(key cacheKey) (cacheEntry, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	entry, ok := c.entries[key]

	return entry, ok
}

// set stores entry as the first page of a fresh listing for key, discarding
// anything previously cached under it.
func (c *listCache) set(key cacheKey, entry cacheEntry) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.entries[key] = entry
}

// appendPage appends a newly fetched page's entries to whatever is already
// cached for key (or starts a fresh entry if none exists yet), and updates
// the pagination state to reflect the new page.
func (c *listCache) appendPage(key cacheKey, page []domain.ObjectEntry, nextToken string, isTruncated bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	existing := c.entries[key]

	merged := make([]domain.ObjectEntry, 0, len(existing.Entries)+len(page))
	merged = append(merged, existing.Entries...)
	merged = append(merged, page...)

	c.entries[key] = cacheEntry{
		Entries:     merged,
		NextToken:   nextToken,
		IsTruncated: isTruncated,
	}
}

// invalidate discards any cached listing for key.
func (c *listCache) invalidate(key cacheKey) {
	c.mu.Lock()
	defer c.mu.Unlock()

	delete(c.entries, key)
}
