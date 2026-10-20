package channel

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	cryptorand "crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// The contract caps raw payload retention at 30 days, encrypted on local
// storage, and keeps payload bytes out of routine backups. This store honors
// that by being a filesystem object store — never a gorm table — so database
// backups stay content-free and the retention sweep is a directory walk, not
// a DELETE over a table sharing a WAL with tenant data. Each object is
// AES-256-GCM ciphertext (nonce-prefixed) beside a JSON sidecar holding the
// scope and expiry that triage needs without decrypting anything.

// LocalPayloadStoreName identifies refs this store can open.
const LocalPayloadStoreName = "local-encrypted"

// RawPayloadContentType is the sidecar content type for captured platform
// payloads, which are the raw HTTP JSON bodies.
const RawPayloadContentType = "application/json"

// defaultPayloadRetention is the contract's ceiling: 30 days maximum.
const defaultPayloadRetention = 30 * 24 * time.Hour

// payloadObjectIDPattern pins object ids to 16 random bytes hex-encoded; it
// doubles as the path traversal guard for every id coming off a stored row.
var payloadObjectIDPattern = regexp.MustCompile(`^[0-9a-f]{32}$`)

// Sentinels distinguish "wrong store" (caller routing bug) from "gone"
// (expired or never existed) from "tampered" (integrity failure).
var (
	ErrPayloadStoreMismatch = errors.New("chat payload ref belongs to another store")
	ErrPayloadNotFound      = errors.New("chat payload object not found")
	ErrPayloadCorrupt       = errors.New("chat payload object failed integrity check")
)

// PayloadMeta is the plaintext sidecar describing one stored object. It is
// written unencrypted on purpose: retention sweeps and triage scope checks
// must run without the key, while the payload itself stays sealed.
type PayloadMeta struct {
	TenantID       string    `json:"tenant_id"`
	WorkspaceID    string    `json:"workspace_id"`
	Platform       string    `json:"platform"`
	InstallationID string    `json:"installation_id"`
	EventID        string    `json:"event_id"`
	ContentType    string    `json:"content_type"`
	CreatedAt      time.Time `json:"created_at"`
	ExpiresAt      time.Time `json:"expires_at"`
}

// LocalPayloadStore keeps encrypted raw payloads under one root directory.
// The constructor validates the key; retention <= 0 means the contract's 30d
// default. now/random are seams so tests can age objects and fail entropy.
type LocalPayloadStore struct {
	root      string
	key       []byte
	retention time.Duration
	now       func() time.Time
	random    func(int) ([]byte, error)
}

// NewLocalPayloadStore returns a store rooted at root. The key must be 32
// bytes (AES-256). The root is created lazily on first save so a misconfigured
// path surfaces at use rather than at startup, and read-only tools can still
// be constructed against a path that does not exist.
func NewLocalPayloadStore(root string, key []byte, retention time.Duration) (*LocalPayloadStore, error) {
	if len(key) != 32 {
		return nil, fmt.Errorf("chat payload key must be 32 bytes for AES-256, got %d", len(key))
	}
	if retention <= 0 {
		retention = defaultPayloadRetention
	}
	return &LocalPayloadStore{
		root:      root,
		key:       key,
		retention: retention,
		now:       time.Now,
		random:    cryptRandRead,
	}, nil
}

// cryptRandRead keeps crypto/rand behind the store's seam signature; the
// error flows to the caller without a separate branch (crypto/rand failures
// are effectively unreachable here, and a branch-only-for-lint is dead code).
func cryptRandRead(n int) ([]byte, error) {
	buf := make([]byte, n)
	_, err := cryptorand.Read(buf)
	return buf, err
}

// sha256Bytes is the integrity anchor over plaintext, which lets a swapped
// object be caught from the row alone.
func sha256Bytes(b []byte) []byte {
	sum := sha256.Sum256(b)
	return sum[:]
}

// aead builds the cipher per operation. Key length is the only failure mode
// after constructor validation, and the branch stays live as defense in depth
// against future key handling drift.
func (s *LocalPayloadStore) aead() (cipher.AEAD, error) {
	block, err := aes.NewCipher(s.key)
	if err != nil {
		return nil, fmt.Errorf("chat payload cipher: %w", err)
	}
	return cipher.NewGCM(block)
}

// pathsFor resolves one ref to (object id, object path) after validating the
// store name and object id. Every read takes this path so a path-like object
// id from a tampered row cannot escape the root.
func (s *LocalPayloadStore) pathsFor(ref RawPayloadRef) (string, string, error) {
	if ref.Store != LocalPayloadStoreName {
		return "", "", ErrPayloadStoreMismatch
	}
	if !payloadObjectIDPattern.MatchString(ref.ObjectID) {
		return "", "", ErrPayloadNotFound
	}
	return ref.ObjectID, filepath.Join(s.root, ref.ObjectID), nil
}

// Save seals data and its sidecar under root, returning the ref to stamp on
// the inbox row.
func (s *LocalPayloadStore) Save(ctx context.Context, meta PayloadMeta, data []byte) (RawPayloadRef, error) {
	if err := ctx.Err(); err != nil {
		return RawPayloadRef{}, fmt.Errorf("chat payload save: %w", err)
	}
	gcm, err := s.aead()
	if err != nil {
		return RawPayloadRef{}, err
	}
	if err := os.MkdirAll(s.root, 0o700); err != nil {
		return RawPayloadRef{}, fmt.Errorf("chat payload root: %w", err)
	}
	idBytes, err := s.random(16)
	if err != nil {
		return RawPayloadRef{}, fmt.Errorf("chat payload object id: %w", err)
	}
	objectID := hex.EncodeToString(idBytes)
	nonce, err := s.random(gcm.NonceSize())
	if err != nil {
		return RawPayloadRef{}, fmt.Errorf("chat payload nonce: %w", err)
	}

	now := s.now()
	meta.CreatedAt = now
	meta.ExpiresAt = now.Add(s.retention)
	if meta.ContentType == "" {
		meta.ContentType = RawPayloadContentType
	}

	objectPath := filepath.Join(s.root, objectID)
	if err := os.WriteFile(objectPath, append(nonce, gcm.Seal(nil, nonce, data, nil)...), 0o600); err != nil {
		return RawPayloadRef{}, fmt.Errorf("chat payload write: %w", err)
	}
	metaBytes, err := json.Marshal(meta)
	if err == nil {
		err = os.WriteFile(s.metaPath(objectID), metaBytes, 0o600)
	}
	if err != nil {
		_ = os.Remove(objectPath) // never leave ciphertext without its sidecar
		return RawPayloadRef{}, fmt.Errorf("chat payload meta write: %w", err)
	}
	return RawPayloadRef{
		Store:     LocalPayloadStoreName,
		ObjectID:  objectID,
		SHA256:    hex.EncodeToString(sha256Bytes(data)),
		ExpiresAt: meta.ExpiresAt,
	}, nil
}

func (s *LocalPayloadStore) metaPath(objectID string) string {
	return filepath.Join(s.root, objectID+".meta.json")
}

// Open unseals one object and re-checks its hash against the ref.
func (s *LocalPayloadStore) Open(ctx context.Context, ref RawPayloadRef) ([]byte, error) {
	_, path, err := s.pathsFor(ref)
	if err != nil {
		return nil, err
	}
	blob, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrPayloadNotFound
		}
		return nil, fmt.Errorf("chat payload read: %w", err)
	}
	gcm, err := s.aead()
	if err != nil {
		return nil, err
	}
	if len(blob) < gcm.NonceSize() {
		return nil, ErrPayloadCorrupt
	}
	plain, err := gcm.Open(nil, blob[:gcm.NonceSize()], blob[gcm.NonceSize():], nil)
	if err != nil {
		return nil, ErrPayloadCorrupt
	}
	if hex.EncodeToString(sha256Bytes(plain)) != ref.SHA256 {
		return nil, ErrPayloadCorrupt
	}
	return plain, nil
}

// Meta returns the plaintext sidecar for one object.
func (s *LocalPayloadStore) Meta(ctx context.Context, ref RawPayloadRef) (PayloadMeta, error) {
	objectID, _, err := s.pathsFor(ref)
	if err != nil {
		return PayloadMeta{}, err
	}
	raw, err := os.ReadFile(s.metaPath(objectID))
	if err != nil {
		if os.IsNotExist(err) {
			return PayloadMeta{}, ErrPayloadNotFound
		}
		return PayloadMeta{}, fmt.Errorf("chat payload meta read: %w", err)
	}
	var meta PayloadMeta
	if err := json.Unmarshal(raw, &meta); err != nil {
		return PayloadMeta{}, fmt.Errorf("chat payload meta decode: %w", err)
	}
	return meta, nil
}

// Delete removes an object and its sidecar. Tenant-requested early deletion
// routes through here; the retention sweep removes the same pair.
func (s *LocalPayloadStore) Delete(ctx context.Context, ref RawPayloadRef) error {
	_, path, err := s.pathsFor(ref)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil {
		if os.IsNotExist(err) {
			return ErrPayloadNotFound
		}
		return fmt.Errorf("chat payload delete: %w", err)
	}
	_ = os.Remove(s.metaPath(filepath.Base(path)))
	return nil
}

// Purge walks the root and removes every object whose sidecar says it expired
// before now, returning how many were removed. A sidecar that cannot be read
// or parsed counts as expired: an undescribable object is untriageable, and
// keeping it forever would outrun the contract's 30d ceiling. Objects whose
// removal fails are skipped — the sidecar stays so the next sweep retries —
// rather than aborting the walk, and non-sidecar files are left untouched.
func (s *LocalPayloadStore) Purge(ctx context.Context, now time.Time) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, fmt.Errorf("chat payload purge: %w", err)
	}
	if _, err := os.Stat(s.root); err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, fmt.Errorf("chat payload root: %w", err)
	}
	entries, err := os.ReadDir(s.root)
	if err != nil {
		return 0, fmt.Errorf("chat payload root: %w", err)
	}
	removed := 0
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(name, ".meta.json") {
			continue
		}
		objectID := strings.TrimSuffix(name, ".meta.json")
		var meta PayloadMeta
		expired := true // unreadable or undescribable sidecars count as expired
		if raw, err := os.ReadFile(filepath.Join(s.root, name)); err == nil {
			if json.Unmarshal(raw, &meta) == nil {
				expired = meta.ExpiresAt.Before(now)
			}
		}
		if !expired {
			continue
		}
		if err := os.Remove(filepath.Join(s.root, objectID)); err != nil && !os.IsNotExist(err) {
			continue
		}
		_ = os.Remove(filepath.Join(s.root, name))
		removed++
	}
	return removed, nil
}
