// Package sessionbackup creates encrypted, local-only browser-session backups.
//
// Cookie values are deliberately accepted only as in-memory snapshot fields and
// are written exclusively inside an authenticated encrypted envelope. Callers
// must never log a Snapshot or return it from an HTTP response.
package sessionbackup

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/argon2"
)

const (
	FormatVersion   = 1
	RetentionDays   = 35
	MaxArchiveBytes = 8 << 20
	maxCookies      = 500
	keyBytes        = 32
	saltBytes       = 16
)

var (
	ErrInvalidPassphrase = errors.New("invalid session backup passphrase")
	ErrInvalidSnapshot   = errors.New("invalid session snapshot")
	ErrInvalidArchive    = errors.New("invalid session backup archive")
	ErrArchiveTooLarge   = errors.New("session backup archive is too large")
)

// Account contains the non-secret metadata recreated on another device.
type Account struct {
	PlatformCode string `json:"platform_code"`
	Name         string `json:"name"`
	Remark       string `json:"remark"`
	Group        string `json:"group"`
}

// Profile is deliberately limited to fingerprint settings that are required to
// keep an imported browser profile consistent. Proxy configuration never moves
// between devices because it may contain device-specific routing information.
type Profile struct {
	BrowserType       string `json:"browser_type"`
	FingerprintSeed   int    `json:"fingerprint_seed"`
	FingerprintConfig string `json:"fingerprint_config"`
	Lang              string `json:"lang"`
	AcceptLang        string `json:"accept_lang"`
	Timezone          string `json:"timezone"`
}

// Cookie is a CDP cookie representation. Value is secret and must remain
// encrypted at rest and in transit.
type Cookie struct {
	Name               string  `json:"name"`
	Value              string  `json:"value"`
	Domain             string  `json:"domain"`
	Path               string  `json:"path"`
	Expires            float64 `json:"expires"`
	HTTPOnly           bool    `json:"http_only"`
	Secure             bool    `json:"secure"`
	SameSite           string  `json:"same_site"`
	Priority           string  `json:"priority"`
	SourceScheme       string  `json:"source_scheme"`
	SourcePort         int64   `json:"source_port"`
	PartitionKeyTopURL string  `json:"partition_key_top_level_url"`
}

type Snapshot struct {
	Version   int       `json:"version"`
	CreatedAt time.Time `json:"created_at"`
	Account   Account   `json:"account"`
	Profile   Profile   `json:"profile"`
	Cookies   []Cookie  `json:"cookies"`
}

// BackupInfo is the only local-backup data exposed to API/UI consumers.
// It includes no profile path, cookie count, or session data.
type BackupInfo struct {
	AccountID int64     `json:"account_id"`
	CreatedAt time.Time `json:"created_at"`
	ExpiresAt time.Time `json:"expires_at"`
}

type envelope struct {
	Version   int    `json:"version"`
	Mode      string `json:"mode"`
	CreatedAt string `json:"created_at"`
	Salt      []byte `json:"salt,omitempty"`
	Nonce     []byte `json:"nonce"`
	Payload   []byte `json:"payload"`
}

type Store struct {
	root string
	now  func() time.Time
}

func New(root string) (*Store, error) {
	return newStore(root, time.Now)
}

func newStore(root string, now func() time.Time) (*Store, error) {
	if strings.TrimSpace(root) == "" {
		return nil, fmt.Errorf("session backup root is required")
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, fmt.Errorf("create session backup root: %w", err)
	}
	if err := os.Chmod(root, 0o700); err != nil {
		return nil, fmt.Errorf("protect session backup root: %w", err)
	}
	if now == nil {
		now = time.Now
	}
	return &Store{root: root, now: now}, nil
}

// SaveLocal writes a daily encrypted session snapshot and returns only safe
// metadata. A same-day backup replaces the older snapshot after the new file
// has been fully written.
func (s *Store) SaveLocal(accountID int64, snapshot Snapshot) (BackupInfo, error) {
	if accountID <= 0 {
		return BackupInfo{}, fmt.Errorf("%w: account id is required", ErrInvalidSnapshot)
	}
	if err := validateSnapshot(snapshot); err != nil {
		return BackupInfo{}, err
	}
	now := s.now().UTC()
	if snapshot.CreatedAt.IsZero() {
		snapshot.CreatedAt = now
	}
	payload, err := json.Marshal(snapshot)
	if err != nil {
		return BackupInfo{}, fmt.Errorf("encode session snapshot: %w", err)
	}
	key, err := s.localKey()
	if err != nil {
		return BackupInfo{}, err
	}
	archive, err := seal("local", now, nil, key, payload)
	if err != nil {
		return BackupInfo{}, err
	}
	dir, err := s.accountDir(accountID)
	if err != nil {
		return BackupInfo{}, err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return BackupInfo{}, fmt.Errorf("create account backup directory: %w", err)
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return BackupInfo{}, fmt.Errorf("protect account backup directory: %w", err)
	}
	path := filepath.Join(dir, now.Format("2006-01-02")+".anxbackup")
	if err := writePrivateFile(path, archive); err != nil {
		return BackupInfo{}, err
	}
	if err := s.pruneLocal(accountID, now); err != nil {
		return BackupInfo{}, err
	}
	return BackupInfo{AccountID: accountID, CreatedAt: now, ExpiresAt: now.AddDate(0, 0, RetentionDays)}, nil
}

func (s *Store) ListLocal(accountID int64) ([]BackupInfo, error) {
	if accountID <= 0 {
		return nil, fmt.Errorf("account id is required")
	}
	dir, err := s.accountDir(accountID)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return []BackupInfo{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("list session backups: %w", err)
	}
	items := make([]BackupInfo, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".anxbackup") {
			continue
		}
		dateText := strings.TrimSuffix(entry.Name(), ".anxbackup")
		createdAt, err := time.ParseInLocation("2006-01-02", dateText, time.UTC)
		if err != nil {
			continue
		}
		items = append(items, BackupInfo{AccountID: accountID, CreatedAt: createdAt, ExpiresAt: createdAt.AddDate(0, 0, RetentionDays)})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].CreatedAt.After(items[j].CreatedAt) })
	return items, nil
}

// LoadLocal decrypts one local daily snapshot for an explicit restore action.
// The decrypted Snapshot remains in memory only; callers must not log or
// return it from an API response.
func (s *Store) LoadLocal(accountID int64, createdAt time.Time) (Snapshot, error) {
	if accountID <= 0 || createdAt.IsZero() {
		return Snapshot{}, ErrInvalidArchive
	}
	dir, err := s.accountDir(accountID)
	if err != nil {
		return Snapshot{}, err
	}
	path := filepath.Join(dir, createdAt.UTC().Format("2006-01-02")+".anxbackup")
	archive, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return Snapshot{}, ErrInvalidArchive
	}
	if err != nil {
		return Snapshot{}, fmt.Errorf("read local session backup: %w", err)
	}
	if len(archive) == 0 || len(archive) > MaxArchiveBytes {
		return Snapshot{}, ErrInvalidArchive
	}
	var env envelope
	if err := json.Unmarshal(archive, &env); err != nil || env.Version != FormatVersion || env.Mode != "local" {
		return Snapshot{}, ErrInvalidArchive
	}
	key, err := s.localKey()
	if err != nil {
		return Snapshot{}, err
	}
	payload, err := openEnvelope(env, key)
	if err != nil || len(payload) > MaxArchiveBytes {
		return Snapshot{}, ErrInvalidArchive
	}
	var snapshot Snapshot
	if err := json.Unmarshal(payload, &snapshot); err != nil || validateSnapshot(snapshot) != nil {
		return Snapshot{}, ErrInvalidArchive
	}
	return snapshot, nil
}

func (s *Store) EncryptPortable(snapshot Snapshot, passphrase string) ([]byte, error) {
	if err := validateSnapshot(snapshot); err != nil {
		return nil, err
	}
	if err := validatePassphrase(passphrase); err != nil {
		return nil, err
	}
	if snapshot.CreatedAt.IsZero() {
		snapshot.CreatedAt = s.now().UTC()
	}
	payload, err := json.Marshal(snapshot)
	if err != nil {
		return nil, fmt.Errorf("encode portable session: %w", err)
	}
	salt := make([]byte, saltBytes)
	if _, err := io.ReadFull(rand.Reader, salt); err != nil {
		return nil, fmt.Errorf("generate portable session salt: %w", err)
	}
	return seal("portable", snapshot.CreatedAt.UTC(), salt, portableKey(passphrase, salt), payload)
}

func (s *Store) DecryptPortable(archive []byte, passphrase string) (Snapshot, error) {
	if len(archive) == 0 {
		return Snapshot{}, ErrInvalidArchive
	}
	if len(archive) > MaxArchiveBytes {
		return Snapshot{}, ErrArchiveTooLarge
	}
	if err := validatePassphrase(passphrase); err != nil {
		return Snapshot{}, err
	}
	var env envelope
	if err := json.Unmarshal(archive, &env); err != nil || env.Version != FormatVersion || env.Mode != "portable" || len(env.Salt) != saltBytes {
		return Snapshot{}, ErrInvalidArchive
	}
	payload, err := openEnvelope(env, portableKey(passphrase, env.Salt))
	if err != nil {
		// Authentication failure intentionally does not reveal archive internals.
		return Snapshot{}, ErrInvalidPassphrase
	}
	if len(payload) > MaxArchiveBytes {
		return Snapshot{}, ErrArchiveTooLarge
	}
	var snapshot Snapshot
	if err := json.Unmarshal(payload, &snapshot); err != nil {
		return Snapshot{}, ErrInvalidArchive
	}
	if err := validateSnapshot(snapshot); err != nil {
		return Snapshot{}, ErrInvalidArchive
	}
	return snapshot, nil
}

func (s *Store) accountDir(accountID int64) (string, error) {
	if accountID <= 0 {
		return "", fmt.Errorf("account id is required")
	}
	return filepath.Join(s.root, "account-"+strconv.FormatInt(accountID, 10)), nil
}

func (s *Store) localKey() ([]byte, error) {
	path := filepath.Join(s.root, ".local-key")
	key, err := os.ReadFile(path)
	if err == nil {
		if len(key) != keyBytes {
			return nil, fmt.Errorf("invalid local session backup key")
		}
		return key, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("read local session backup key: %w", err)
	}
	key = make([]byte, keyBytes)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		return nil, fmt.Errorf("generate local session backup key: %w", err)
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, os.ErrExist) {
		return s.localKey()
	}
	if err != nil {
		return nil, fmt.Errorf("create local session backup key: %w", err)
	}
	_, writeErr := file.Write(key)
	closeErr := file.Close()
	if writeErr != nil || closeErr != nil {
		return nil, fmt.Errorf("write local session backup key")
	}
	return key, nil
}

func (s *Store) pruneLocal(accountID int64, now time.Time) error {
	backups, err := s.ListLocal(accountID)
	if err != nil {
		return err
	}
	dir, _ := s.accountDir(accountID)
	for _, backup := range backups {
		if !backup.ExpiresAt.Before(now.UTC()) {
			continue
		}
		path := filepath.Join(dir, backup.CreatedAt.UTC().Format("2006-01-02")+".anxbackup")
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("prune expired session backup: %w", err)
		}
	}
	return nil
}

func validateSnapshot(snapshot Snapshot) error {
	if snapshot.Version != FormatVersion || strings.TrimSpace(snapshot.Account.PlatformCode) == "" || len(snapshot.Cookies) == 0 || len(snapshot.Cookies) > maxCookies {
		return ErrInvalidSnapshot
	}
	for _, cookie := range snapshot.Cookies {
		if strings.TrimSpace(cookie.Name) == "" || strings.TrimSpace(cookie.Value) == "" || strings.TrimSpace(cookie.Domain) == "" {
			return ErrInvalidSnapshot
		}
	}
	return nil
}

func validatePassphrase(passphrase string) error {
	if length := len([]rune(strings.TrimSpace(passphrase))); length < 12 || length > 128 {
		return ErrInvalidPassphrase
	}
	return nil
}

func portableKey(passphrase string, salt []byte) []byte {
	return argon2.IDKey([]byte(passphrase), salt, 3, 64*1024, 4, keyBytes)
}

func seal(mode string, createdAt time.Time, salt, key, plaintext []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("initialize session backup encryption: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("initialize session backup encryption: %w", err)
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("generate session backup nonce: %w", err)
	}
	payload := gcm.Seal(nil, nonce, plaintext, []byte(mode))
	return json.Marshal(envelope{Version: FormatVersion, Mode: mode, CreatedAt: createdAt.UTC().Format(time.RFC3339), Salt: salt, Nonce: nonce, Payload: payload})
}

func openEnvelope(env envelope, key []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil || len(env.Nonce) != gcm.NonceSize() {
		return nil, ErrInvalidArchive
	}
	return gcm.Open(nil, env.Nonce, env.Payload, []byte(env.Mode))
}

func writePrivateFile(path string, payload []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".session-*")
	if err != nil {
		return fmt.Errorf("create session backup file: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("protect session backup file: %w", err)
	}
	if _, err := tmp.Write(payload); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write session backup file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close session backup file: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("commit session backup file: %w", err)
	}
	return nil
}
