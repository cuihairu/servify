package channel

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func newPayloadKey(t *testing.T) []byte {
	t.Helper()
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatalf("read random key: %v", err)
	}
	return key
}

// testRandRead matches the store's random seam signature over crypto/rand.
func testRandRead(n int) ([]byte, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return nil, err
	}
	return buf, nil
}

func newPayloadStore(t *testing.T) (*LocalPayloadStore, string) {
	t.Helper()
	root := t.TempDir()
	store, err := NewLocalPayloadStore(root, newPayloadKey(t), 0)
	if err != nil {
		t.Fatalf("new payload store: %v", err)
	}
	return store, root
}

func payloadMeta() PayloadMeta {
	return PayloadMeta{
		TenantID:       "tenant-1",
		WorkspaceID:    "workspace-1",
		Platform:       "telegram",
		InstallationID: "inst-1",
		EventID:        "update-41",
		ContentType:    RawPayloadContentType,
	}
}

func TestNewLocalPayloadStoreRejectsBadKey(t *testing.T) {
	if _, err := NewLocalPayloadStore(t.TempDir(), nil, 0); err == nil {
		t.Error("nil key must be rejected")
	}
	if _, err := NewLocalPayloadStore(t.TempDir(), make([]byte, 16), 0); err == nil {
		t.Error("short key must be rejected (AES-256 wants 32 bytes)")
	}
}

func TestPayloadStoreRoundTripEncryptsAtRest(t *testing.T) {
	store, root := newPayloadStore(t)
	data := []byte(`{"update_id":41,"message":{"text":"hello secret"}}`)

	ref, err := store.Save(context.Background(), payloadMeta(), data)
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	if ref.Store != LocalPayloadStoreName {
		t.Errorf("store name = %q, want %q", ref.Store, LocalPayloadStoreName)
	}
	if len(ref.ObjectID) != 32 || strings.TrimRight(ref.ObjectID, "0123456789abcdef") != "" {
		t.Errorf("object id = %q, want 32 hex chars", ref.ObjectID)
	}
	if want := hex.EncodeToString(sha256Bytes(data)); ref.SHA256 != want {
		t.Errorf("sha256 = %q, want %q", ref.SHA256, want)
	}
	if ref.ExpiresAt.IsZero() {
		t.Error("expires at must be stamped (contract retention)")
	}
	if ttl := time.Until(ref.ExpiresAt); ttl < 29*24*time.Hour || ttl > 30*24*time.Hour {
		t.Errorf("ttl = %v, want the 30d contract default", ttl)
	}

	// encrypted at rest: the ciphertext file must not carry the plaintext
	blob, err := os.ReadFile(filepath.Join(root, ref.ObjectID))
	if err != nil {
		t.Fatalf("read stored object: %v", err)
	}
	if bytes.Contains(blob, data) {
		t.Error("stored object carries plaintext (contract: encrypted on local storage)")
	}
	if info, err := os.Stat(filepath.Join(root, ref.ObjectID)); err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("object perm = %v (%v), want 0600", info.Mode(), err)
	}

	got, err := store.Open(context.Background(), ref)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if !bytes.Equal(got, data) {
		t.Errorf("roundtrip mismatch: %q", got)
	}
	meta, err := store.Meta(context.Background(), ref)
	if err != nil {
		t.Fatalf("meta: %v", err)
	}
	if meta.TenantID != "tenant-1" || meta.WorkspaceID != "workspace-1" || meta.Platform != "telegram" ||
		meta.InstallationID != "inst-1" || meta.EventID != "update-41" || meta.ContentType != RawPayloadContentType ||
		meta.CreatedAt.IsZero() || meta.ExpiresAt.IsZero() {
		t.Errorf("meta drifted: %+v", meta)
	}
}

func TestPayloadStoreCustomRetention(t *testing.T) {
	store, _ := newPayloadStore(t)
	store.retention = time.Hour
	ref, err := store.Save(context.Background(), payloadMeta(), []byte("x"))
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	if ttl := time.Until(ref.ExpiresAt); ttl > time.Hour {
		t.Errorf("ttl = %v, want within 1h", ttl)
	}
}

func TestPayloadStoreOpenRejectsMismatchedStoreAndBadIDs(t *testing.T) {
	store, _ := newPayloadStore(t)
	ctx := context.Background()

	if _, err := store.Open(ctx, RawPayloadRef{Store: "other", ObjectID: strings.Repeat("a", 32)}); !errors.Is(err, ErrPayloadStoreMismatch) {
		t.Errorf("mismatched store: err = %v, want ErrPayloadStoreMismatch", err)
	}
	if _, err := store.Open(ctx, RawPayloadRef{Store: LocalPayloadStoreName, ObjectID: "../escape"}); !errors.Is(err, ErrPayloadNotFound) {
		t.Errorf("path-like object id: err = %v, want ErrPayloadNotFound (traversal guard)", err)
	}
	if _, err := store.Open(ctx, RawPayloadRef{Store: LocalPayloadStoreName}); !errors.Is(err, ErrPayloadNotFound) {
		t.Errorf("empty object id: err = %v, want ErrPayloadNotFound", err)
	}
	if _, err := store.Open(ctx, RawPayloadRef{Store: LocalPayloadStoreName, ObjectID: strings.Repeat("b", 32)}); !errors.Is(err, ErrPayloadNotFound) {
		t.Errorf("unknown object: err = %v, want ErrPayloadNotFound", err)
	}
	if err := store.Delete(ctx, RawPayloadRef{Store: "other"}); !errors.Is(err, ErrPayloadStoreMismatch) {
		t.Errorf("delete mismatched store: err = %v", err)
	}
	if err := store.Delete(ctx, RawPayloadRef{Store: LocalPayloadStoreName, ObjectID: strings.Repeat("c", 32)}); !errors.Is(err, ErrPayloadNotFound) {
		t.Errorf("delete unknown object: err = %v, want ErrPayloadNotFound", err)
	}
	if _, err := store.Meta(ctx, RawPayloadRef{Store: "other"}); !errors.Is(err, ErrPayloadStoreMismatch) {
		t.Errorf("meta mismatched store: err = %v", err)
	}
	if _, err := store.Meta(ctx, RawPayloadRef{Store: LocalPayloadStoreName, ObjectID: strings.Repeat("9", 32)}); !errors.Is(err, ErrPayloadNotFound) {
		t.Errorf("meta unknown object: err = %v, want ErrPayloadNotFound", err)
	}
}

func TestPayloadStoreOpenCorruptAndHashMismatch(t *testing.T) {
	store, root := newPayloadStore(t)
	ctx := context.Background()
	ref, err := store.Save(ctx, payloadMeta(), []byte("payload bytes"))
	if err != nil {
		t.Fatalf("save: %v", err)
	}

	path := filepath.Join(root, ref.ObjectID)
	blob, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	tampered := append([]byte(nil), blob...)
	tampered[len(tampered)-1] ^= 0xFF // flip one ciphertext bit
	if err := os.WriteFile(path, tampered, 0o600); err != nil {
		t.Fatalf("tamper: %v", err)
	}
	if _, err := store.Open(ctx, ref); !errors.Is(err, ErrPayloadCorrupt) {
		t.Errorf("tampered object: err = %v, want ErrPayloadCorrupt", err)
	}

	// a tampered ref hash must not pass either (integrity anchor on the row):
	// the ciphertext itself is restored intact so the GCM tag passes and the
	// hash check is what catches the swap
	if err := os.WriteFile(path, blob, 0o600); err != nil {
		t.Fatalf("restore: %v", err)
	}
	bad := ref
	bad.SHA256 = strings.Repeat("0", 64)
	if _, err := store.Open(ctx, bad); !errors.Is(err, ErrPayloadCorrupt) {
		t.Errorf("hash mismatch: err = %v, want ErrPayloadCorrupt", err)
	}

	// a truncated object (no room even for the nonce) is corrupt, not missing
	shortID := strings.Repeat("a", 32)
	if err := os.WriteFile(filepath.Join(root, shortID), []byte("1234"), 0o600); err != nil {
		t.Fatalf("seed short: %v", err)
	}
	if _, err := store.Open(ctx, RawPayloadRef{Store: LocalPayloadStoreName, ObjectID: shortID, SHA256: "x"}); !errors.Is(err, ErrPayloadCorrupt) {
		t.Errorf("short object: err = %v, want ErrPayloadCorrupt", err)
	}
}

func TestPayloadStoreOpenDirectoryAndBrokenMeta(t *testing.T) {
	store, root := newPayloadStore(t)
	ctx := context.Background()
	id := strings.Repeat("d", 32)
	if err := os.Mkdir(filepath.Join(root, id), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	ref := RawPayloadRef{Store: LocalPayloadStoreName, ObjectID: id}
	if _, err := store.Open(ctx, ref); err == nil || errors.Is(err, ErrPayloadNotFound) {
		t.Errorf("open directory: err = %v, want a wrapped read error", err)
	}
	if err := os.WriteFile(filepath.Join(root, id+".meta.json"), []byte("{not json"), 0o600); err != nil {
		t.Fatalf("write meta: %v", err)
	}
	if _, err := store.Meta(ctx, ref); err == nil {
		t.Error("broken meta must error")
	}

	// a meta path that became a directory surfaces the read error
	if err := os.Remove(filepath.Join(root, ref.ObjectID+".meta.json")); err != nil {
		t.Fatalf("clear meta: %v", err)
	}
	if err := os.Mkdir(filepath.Join(root, ref.ObjectID+".meta.json"), 0o700); err != nil {
		t.Fatalf("seed meta dir: %v", err)
	}
	if _, err := store.Meta(ctx, ref); err == nil || errors.Is(err, ErrPayloadNotFound) {
		t.Errorf("meta as dir: err = %v, want a wrapped read error", err)
	}
}

func TestPayloadStoreDeleteRemovesPair(t *testing.T) {
	store, root := newPayloadStore(t)
	ctx := context.Background()
	ref, err := store.Save(ctx, payloadMeta(), []byte("bye"))
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	if err := store.Delete(ctx, ref); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, ref.ObjectID)); !os.IsNotExist(err) {
		t.Errorf("object still on disk: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, ref.ObjectID+".meta.json")); !os.IsNotExist(err) {
		t.Errorf("meta still on disk: %v", err)
	}
	if _, err := store.Open(ctx, ref); !errors.Is(err, ErrPayloadNotFound) {
		t.Errorf("open after delete: err = %v", err)
	}

	// an object path that became a non-empty directory surfaces the removal error
	stuckID := strings.Repeat("e", 32)
	if err := os.Mkdir(filepath.Join(root, stuckID), 0o700); err != nil {
		t.Fatalf("seed dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, stuckID, "inner"), []byte("x"), 0o600); err != nil {
		t.Fatalf("seed inner: %v", err)
	}
	if err := store.Delete(ctx, RawPayloadRef{Store: LocalPayloadStoreName, ObjectID: stuckID}); err == nil || errors.Is(err, ErrPayloadNotFound) {
		t.Errorf("delete nonempty dir: err = %v, want a wrapped removal error", err)
	}
}

func TestPayloadStoreSaveErrors(t *testing.T) {
	// canceled context surfaces as-is
	store, _ := newPayloadStore(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := store.Save(ctx, payloadMeta(), []byte("x")); !errors.Is(err, context.Canceled) {
		t.Errorf("canceled save: err = %v, want context.Canceled", err)
	}

	// root under an existing file cannot be created
	file := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatalf("seed file: %v", err)
	}
	blocked, err := NewLocalPayloadStore(filepath.Join(file, "payloads"), newPayloadKey(t), 0)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	if _, err := blocked.Save(context.Background(), payloadMeta(), []byte("x")); err == nil {
		t.Error("save into uncreatable root must error")
	}

	// a failing random source fails the save (object id)
	flaky, _ := newPayloadStore(t)
	flaky.random = func(n int) ([]byte, error) { return nil, errors.New("entropy down") }
	if _, err := flaky.Save(context.Background(), payloadMeta(), []byte("x")); err == nil {
		t.Error("save with failing random must error")
	}

	// entropy dying on the second draw fails the nonce save
	entropic, _ := newPayloadStore(t)
	calls := 0
	entropic.random = func(n int) ([]byte, error) {
		calls++
		if calls > 1 {
			return nil, errors.New("entropy down")
		}
		return make([]byte, n), nil
	}
	if _, err := entropic.Save(context.Background(), payloadMeta(), []byte("x")); err == nil {
		t.Error("save with failing nonce draw must error")
	}

	// an omitted content type falls back to the platform payload default
	blankMeta, _ := newPayloadStore(t)
	blankRef, err := blankMeta.Save(context.Background(), PayloadMeta{TenantID: "tenant-1"}, []byte("x"))
	if err != nil {
		t.Fatalf("save blank meta: %v", err)
	}
	if meta, err := blankMeta.Meta(context.Background(), blankRef); err != nil || meta.ContentType != RawPayloadContentType {
		t.Errorf("content type = %q (%v), want %q", meta.ContentType, err, RawPayloadContentType)
	}

	// a sidecar write failure removes the orphan ciphertext (meta name pre-taken by a dir)
	det, detRoot := newPayloadStore(t)
	det.random = func(n int) ([]byte, error) { return make([]byte, n), nil } // deterministic id: 32 zeros
	zeros := strings.Repeat("0", 32)
	if err := os.Mkdir(filepath.Join(detRoot, zeros+".meta.json"), 0o700); err != nil {
		t.Fatalf("seed meta dir: %v", err)
	}
	if _, err := det.Save(context.Background(), payloadMeta(), []byte("x")); err == nil {
		t.Error("save with unwritable meta must error")
	}
	if _, err := os.Stat(filepath.Join(detRoot, zeros)); !os.IsNotExist(err) {
		t.Errorf("orphan ciphertext left behind: %v", err)
	}

	// a key that cannot build the cipher fails at use, past the constructor
	// (defense-in-depth branch stays exercised)
	badKey := &LocalPayloadStore{root: filepath.Join(t.TempDir(), "p"), key: make([]byte, 17),
		retention: time.Hour, now: time.Now, random: testRandRead}
	if _, err := badKey.Save(context.Background(), payloadMeta(), []byte("x")); err == nil {
		t.Error("save with unusable key must error")
	}
	seedRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(seedRoot, zeros), []byte("seeded"), 0o600); err != nil {
		t.Fatalf("seed object: %v", err)
	}
	openBad := &LocalPayloadStore{root: seedRoot, key: make([]byte, 17), retention: time.Hour,
		now: time.Now, random: testRandRead}
	if _, err := openBad.Open(context.Background(), RawPayloadRef{Store: LocalPayloadStoreName, ObjectID: zeros, SHA256: "x"}); err == nil {
		t.Error("open with unusable key must error")
	}

	// read-only root fails the ciphertext write (skip where perms are not enforced)
	if os.Geteuid() == 0 {
		t.Skip("permission-based write failure needs a non-root runner")
	}
	ro, root := newPayloadStore(t)
	if err := os.Chmod(root, 0o500); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(root, 0o700) })
	if _, err := ro.Save(context.Background(), payloadMeta(), []byte("x")); err == nil {
		t.Error("save into read-only root must error")
	}
}

func TestPayloadStorePurge(t *testing.T) {
	store, root := newPayloadStore(t)
	ctx := context.Background()

	// one object stored "31 days ago" (expired), one fresh
	past := time.Now().Add(-31 * 24 * time.Hour)
	store.now = func() time.Time { return past }
	oldRef, err := store.Save(ctx, payloadMeta(), []byte("old"))
	if err != nil {
		t.Fatalf("save old: %v", err)
	}
	store.now = time.Now
	newRef, err := store.Save(ctx, payloadMeta(), []byte("new"))
	if err != nil {
		t.Fatalf("save new: %v", err)
	}

	removed, err := store.Purge(ctx, time.Now())
	if err != nil {
		t.Fatalf("purge: %v", err)
	}
	if removed != 1 {
		t.Errorf("removed = %d, want 1 (the expired object)", removed)
	}
	if _, err := store.Open(ctx, oldRef); !errors.Is(err, ErrPayloadNotFound) {
		t.Errorf("expired object survived: %v", err)
	}
	if _, err := store.Open(ctx, newRef); err != nil {
		t.Errorf("fresh object purged: %v", err)
	}

	// a second purge finds nothing
	if removed, err = store.Purge(ctx, time.Now()); err != nil || removed != 0 {
		t.Errorf("second purge = %d (%v), want 0 nil", removed, err)
	}

	// corrupt meta is treated as expired and removed
	corruptID := strings.Repeat("e", 32)
	if err := os.WriteFile(filepath.Join(root, corruptID), []byte("junk"), 0o600); err != nil {
		t.Fatalf("seed object: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, corruptID+".meta.json"), []byte("{broken"), 0o600); err != nil {
		t.Fatalf("seed meta: %v", err)
	}
	if removed, err = store.Purge(ctx, time.Now()); err != nil || removed != 1 {
		t.Errorf("corrupt purge = %d (%v), want 1 nil", removed, err)
	}
	if _, err := os.Stat(filepath.Join(root, corruptID)); !os.IsNotExist(err) {
		t.Errorf("corrupt object survived: %v", err)
	}

	// non-meta entries are left alone
	if err := os.WriteFile(filepath.Join(root, "README"), []byte("hi"), 0o600); err != nil {
		t.Fatalf("seed readme: %v", err)
	}
	if removed, err = store.Purge(ctx, time.Now()); err != nil || removed != 0 {
		t.Errorf("non-meta purge = %d (%v), want 0 nil", removed, err)
	}

	// an object whose delete fails is not counted (data file became a dir)
	expiredID := strings.Repeat("f", 32)
	if err := os.Mkdir(filepath.Join(root, expiredID), 0o700); err != nil {
		t.Fatalf("seed dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, expiredID, "inner"), []byte("x"), 0o600); err != nil {
		t.Fatalf("seed inner: %v", err)
	}
	meta := payloadMeta()
	meta.CreatedAt = time.Now().Add(-48 * time.Hour)
	meta.ExpiresAt = time.Now().Add(-time.Hour)
	metaBytes, _ := json.Marshal(meta) // strings and time.Time only: cannot fail
	if err := os.WriteFile(filepath.Join(root, expiredID+".meta.json"), metaBytes, 0o600); err != nil {
		t.Fatalf("seed expired meta: %v", err)
	}
	if removed, err = store.Purge(ctx, time.Now()); err != nil || removed != 0 {
		t.Errorf("stuck purge = %d (%v), want 0 nil (delete error not counted)", removed, err)
	}
}

func TestPayloadStorePurgeMissingRootAndContext(t *testing.T) {
	store := &LocalPayloadStore{root: filepath.Join(t.TempDir(), "never-created"),
		key: newPayloadKey(t), retention: time.Hour, now: time.Now, random: testRandRead}
	if removed, err := store.Purge(context.Background(), time.Now()); err != nil || removed != 0 {
		t.Errorf("purge with no root = %d (%v), want 0 nil", removed, err)
	}

	// root as a file: ReadDir error surfaces
	fileRoot := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(fileRoot, []byte("x"), 0o600); err != nil {
		t.Fatalf("seed file: %v", err)
	}
	fileStore := &LocalPayloadStore{root: fileRoot, key: newPayloadKey(t), retention: time.Hour,
		now: time.Now, random: testRandRead}
	if _, err := fileStore.Purge(context.Background(), time.Now()); err == nil {
		t.Error("purge over a file root must error")
	}

	// a root whose parent denies access surfaces the stat error
	// (skip where permissions are not enforced)
	if os.Geteuid() == 0 {
		t.Skip("stat-based purge error needs a non-root runner")
	}
	parent := filepath.Join(t.TempDir(), "guard")
	if err := os.Mkdir(parent, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	guardStore := &LocalPayloadStore{root: filepath.Join(parent, "payloads"), key: newPayloadKey(t),
		retention: time.Hour, now: time.Now, random: testRandRead}
	if err := os.Chmod(parent, 0o000); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(parent, 0o700) })
	if _, err := guardStore.Purge(context.Background(), time.Now()); err == nil {
		t.Error("purge with unstattable root must error")
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := store.Purge(ctx, time.Now()); !errors.Is(err, context.Canceled) {
		t.Errorf("canceled purge: err = %v, want context.Canceled", err)
	}
}
