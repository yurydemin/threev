package s3client

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"sort"
	"sync"

	"github.com/aws/aws-sdk-go-v2/service/s3"

	"threev/internal/crypto"
	"threev/internal/domain"
	"threev/internal/storage"
)

// ConnectionManager owns the long-lived, pooled and fresh S3 clients used by
// the Transfer Engine (docs/02-tech-spec.md section 10.1), one pair per
// connection profile. Unlike NewS3Client/NewS3ClientWithHTTPClient (used
// directly by ConnectionService/FileManagerService for one-off calls, each
// building a fresh client), the clients handed out here are cached and
// reused across many worker goroutines/calls, backed by *http.Transport
// instances tuned for connection reuse (pooled, docs/02-tech-spec.md
// section 10.1) or forced-fresh connections (fresh, used by the future
// retry layer per section 10.4).
//
// It deliberately does not depend on connection.ConnectionService, and -
// unlike FileManagerService - it cannot even depend on the
// connection.ResolveProfile helper package-level function: connection
// already imports s3client (connection/tester.go uses
// s3client.NewS3Client/ClassifyError), so the reverse import would be a
// cycle. Instead it resolves/decrypts profiles itself, duplicating
// ResolveProfile's small amount of logic directly against
// *storage.ProfileRepository and the crypto package.
type ConnectionManager struct {
	repo   *storage.ProfileRepository
	keyBox *crypto.KeyBox

	mu      sync.Mutex
	entries map[int64]*managerEntry
}

// managerEntry caches the pooled/fresh clients built for one profile,
// together with the *http.Transport instances backing them (needed to
// close idle connections on invalidation/replacement) and the fingerprint
// hash of the profile fields that affect them.
type managerEntry struct {
	profileHash string

	pooledClient    *s3.Client
	freshClient     *s3.Client
	pooledTransport *http.Transport
	freshTransport  *http.Transport
}

// NewConnectionManager returns a ConnectionManager backed by repo, reading
// the current encryption key from keyBox on every Get call rather than
// taking a fixed [32]byte at construction time - see crypto.KeyBox's own
// doc comment and NewConnectionService's identical rationale (Этап 4
// суб-этап 4.4, KeyBox). keyBox is expected to be the same instance shared
// with every other service app.go's newApp constructs.
func NewConnectionManager(repo *storage.ProfileRepository, keyBox *crypto.KeyBox) *ConnectionManager {
	return &ConnectionManager{
		repo:    repo,
		keyBox:  keyBox,
		entries: make(map[int64]*managerEntry),
	}
}

// Get returns the pooled and fresh *s3.Client for the given profile,
// building and caching them on first use. On every call the profile is
// re-loaded and its transport-affecting fields are fingerprinted
// (profileHash); if the fingerprint differs from the one cached alongside
// the existing entry (i.e. the profile was edited since the clients were
// built), the stale entry's transports have their idle connections closed
// and both clients are rebuilt from the current profile. There is no
// separate change-notification path (e.g. from ConnectionService) - this
// lazy, hash-based comparison on every Get is the sole invalidation
// mechanism besides the explicit Invalidate below.
//
// Get is safe for concurrent use by multiple goroutines: it is expected to
// be called from many worker goroutines, both within one transfer's worker
// pool and across multiple concurrent transfers against the same or
// different profiles.
//
// Guarded (Этап 4 суб-этап 4.4): building a client requires decrypting the
// profile's SecretAccessKey/SessionToken, which requires the current
// encryption key - unavailable while the application is locked. See
// domain.ErrLocked's own doc comment.
func (m *ConnectionManager) Get(profileID int64) (pooled *s3.Client, fresh *s3.Client, err error) {
	key, ok := m.keyBox.Get()
	if !ok {
		return nil, nil, domain.ErrLocked
	}

	p, err := m.resolveProfile(profileID, key)
	if err != nil {
		return nil, nil, fmt.Errorf("resolve profile %d: %w", profileID, err)
	}

	hash := profileHash(p)

	m.mu.Lock()
	defer m.mu.Unlock()

	if entry, ok := m.entries[profileID]; ok {
		if entry.profileHash == hash {
			return entry.pooledClient, entry.freshClient, nil
		}

		closeEntry(entry)
		delete(m.entries, profileID)
	}

	entry, err := newManagerEntry(p, hash)
	if err != nil {
		return nil, nil, fmt.Errorf("build S3 clients for profile %d: %w", profileID, err)
	}

	m.entries[profileID] = entry

	return entry.pooledClient, entry.freshClient, nil
}

// Invalidate discards the cached clients (if any) for profileID, closing
// their transports' idle connections. The next Get call for this profile
// rebuilds both clients from the current profile data. This is an explicit
// counterpart to Get's automatic, hash-based invalidation: it is not
// required for correctness (Get would eventually notice a changed profile
// on its own), but lets a caller that knows a profile just changed force
// the rebuild to happen eagerly rather than on the next incidental Get.
func (m *ConnectionManager) Invalidate(profileID int64) {
	m.mu.Lock()
	defer m.mu.Unlock()

	entry, ok := m.entries[profileID]
	if !ok {
		return
	}

	closeEntry(entry)
	delete(m.entries, profileID)
}

// resolveProfile loads the profile identified by id from m.repo and
// decrypts its SecretAccessKey and (if present) SessionToken using key. It
// is functionally identical to connection.ResolveProfile, duplicated here
// rather than called directly because connection already imports s3client
// (see the ConnectionManager doc comment) and importing it back would
// create a cycle.
//
// key is passed in by Get (already guarded/read from m.keyBox) rather than
// read from m.keyBox itself here - this private helper trusts its one
// caller to have already performed the domain.ErrLocked guard, mirroring
// connection.ResolveProfile's own signature (a raw [32]byte parameter, not
// a *crypto.KeyBox).
func (m *ConnectionManager) resolveProfile(id int64, key [32]byte) (domain.Profile, error) {
	p, err := m.repo.GetByID(context.Background(), id)
	if err != nil {
		return domain.Profile{}, fmt.Errorf("get profile %d: %w", id, err)
	}

	secret, err := crypto.Decrypt(p.SecretAccessKey, key)
	if err != nil {
		return domain.Profile{}, fmt.Errorf("get profile %d: decrypt secret access key: %w", id, err)
	}

	p.SecretAccessKey = string(secret)

	if p.SessionToken != "" {
		token, err := crypto.Decrypt(p.SessionToken, key)
		if err != nil {
			return domain.Profile{}, fmt.Errorf("get profile %d: decrypt session token: %w", id, err)
		}

		p.SessionToken = string(token)
	}

	return p, nil
}

// newManagerEntry builds the pooled and fresh transports/clients for
// profile p, tagging the resulting entry with hash.
func newManagerEntry(p domain.Profile, hash string) (*managerEntry, error) {
	pooledTransport, err := newPooledTransport(p)
	if err != nil {
		return nil, fmt.Errorf("build pooled transport: %w", err)
	}

	freshTransport, err := newFreshTransport(p)
	if err != nil {
		return nil, fmt.Errorf("build fresh transport: %w", err)
	}

	pooledClient, err := NewS3ClientWithHTTPClient(p, &http.Client{
		Transport: pooledTransport,
		Timeout:   defaultHTTPTimeout,
	})
	if err != nil {
		return nil, fmt.Errorf("build pooled client: %w", err)
	}

	freshClient, err := NewS3ClientWithHTTPClient(p, &http.Client{
		Transport: freshTransport,
		Timeout:   defaultHTTPTimeout,
	})
	if err != nil {
		return nil, fmt.Errorf("build fresh client: %w", err)
	}

	return &managerEntry{
		profileHash:     hash,
		pooledClient:    pooledClient,
		freshClient:     freshClient,
		pooledTransport: pooledTransport,
		freshTransport:  freshTransport,
	}, nil
}

// closeEntry releases the idle connections held by entry's transports. The
// fresh transport disables keep-alives, so it rarely has idle connections
// to close, but calling CloseIdleConnections on it regardless is cheap and
// safe.
func closeEntry(entry *managerEntry) {
	entry.pooledTransport.CloseIdleConnections()
	entry.freshTransport.CloseIdleConnections()
}

// profileHash returns a stable fingerprint of the profile fields that
// affect how its S3 clients/transports are built: endpoint, region,
// credentials, path style, TLS verification, custom headers, and proxy URL.
// It is not meant to be a secret-safe or cryptographically strong digest -
// it exists purely as a cheap, deterministic way for Get to detect "has this
// profile changed since clients were last cached for it" without keeping a
// full copy of the previous domain.Profile around to compare field by
// field. crypto/sha256 is used only for its convenient fixed-size,
// collision-resistant-enough-for-this-purpose output, not for any security
// property.
func profileHash(p domain.Profile) string {
	h := sha256.New()

	fmt.Fprintf(h, "%s\x00%s\x00%s\x00%s\x00%s\x00%t\x00%t\x00",
		p.EndpointURL, p.Region, p.AccessKeyID, p.SecretAccessKey, p.SessionToken,
		p.PathStyle, p.VerifySSL,
	)

	writeSortedHeaders(h, p.CustomHeaders)

	fmt.Fprintf(h, "%s\x00", p.ProxyURL)

	return hex.EncodeToString(h.Sum(nil))
}

// writeSortedHeaders writes headers into w in a deterministic (sorted by
// key) order, so profileHash is stable regardless of Go's randomized map
// iteration order.
func writeSortedHeaders(w io.Writer, headers map[string]string) {
	keys := make([]string, 0, len(headers))
	for k := range headers {
		keys = append(keys, k)
	}

	sort.Strings(keys)

	for _, k := range keys {
		fmt.Fprintf(w, "%s\x01%s\x00", k, headers[k])
	}
}
