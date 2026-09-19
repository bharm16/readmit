package hub

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
)

const MaxArtifactBytes int64 = 64 << 20
const schemaVersion = 5
const lockID int64 = 0x726561646d6974

var ErrConflict = errors.New("another hub or maintenance operation owns this database")
var ErrMissing = errors.New("artifact unavailable")
var ErrIntegrity = errors.New("artifact integrity failure")
var ErrLimit = errors.New("storage limit exceeded")

// Store holds an exclusive database lease for its entire lifetime. One service
// owns one local artifact root; replicas and shared network volumes are refused.
type Store struct {
	db     *sql.DB
	lease  *sql.Conn
	root   *os.Root
	config Config
	mu     sync.Mutex
}

func Open(ctx context.Context, c Config) (*Store, error) {
	if !filepath.IsAbs(c.Socket) || c.Port == 0 || c.Database == "" || c.User == "" || !filepath.IsAbs(c.Root) || c.MaxBytes < MaxArtifactBytes {
		return nil, errors.New("invalid storage configuration")
	}
	// Explicit values replace all libpq environment defaults, including passwords,
	// service files, fallback addresses, and session initialization parameters.
	pc, err := pgx.ParseConfig("")
	if err != nil {
		return nil, errors.New("database configuration unavailable")
	}
	pc.Host = c.Socket
	pc.Port = c.Port
	pc.Database = c.Database
	pc.User = c.User
	pc.Password = ""
	pc.TLSConfig = nil
	pc.Fallbacks = nil
	pc.RuntimeParams = map[string]string{}
	pc.ConnectTimeout = 5 * time.Second
	db := stdlib.OpenDB(*pc)
	db.SetMaxOpenConns(5)
	lease, err := db.Conn(ctx)
	if err != nil {
		db.Close()
		return nil, errors.New("database unavailable")
	}
	s := &Store{db: db, lease: lease, config: c}
	var locked bool
	if err = lease.QueryRowContext(ctx, "SELECT pg_try_advisory_lock($1)", lockID).Scan(&locked); err != nil || !locked {
		s.Close()
		return nil, ErrConflict
	}
	if err = os.MkdirAll(c.Root, 0700); err != nil {
		s.Close()
		return nil, errors.New("artifact root unavailable")
	}
	info, err := os.Lstat(c.Root)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0077 != 0 {
		s.Close()
		return nil, errors.New("artifact root must be a private directory")
	}
	s.root, err = os.OpenRoot(c.Root)
	if err != nil {
		s.Close()
		return nil, errors.New("artifact root unavailable")
	}
	return s, nil
}

func (s *Store) Close() error {
	if s.root != nil {
		s.root.Close()
	}
	if s.lease != nil {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		s.lease.ExecContext(ctx, "SELECT pg_advisory_unlock($1)", lockID)
		s.lease.Close()
	}
	if s.db != nil {
		return s.db.Close()
	}
	return nil
}

// Migrate changes shared metadata only. Artifact contracts and bytes never migrate.
func (s *Store) Migrate(ctx context.Context) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS readmit_hub_schema (singleton boolean PRIMARY KEY CHECK(singleton), version integer NOT NULL); INSERT INTO readmit_hub_schema VALUES(true,0) ON CONFLICT DO NOTHING`); err != nil {
		return errors.New("schema initialization failed")
	}
	var version int
	if err = tx.QueryRowContext(ctx, "SELECT version FROM readmit_hub_schema WHERE singleton").Scan(&version); err != nil {
		return errors.New("schema unavailable")
	}
	if version < 0 || version > schemaVersion {
		return errors.New("unsupported metadata schema")
	}
	if version < 1 {
		if _, err = tx.ExecContext(ctx, `CREATE TABLE readmit_hub_artifacts (digest text PRIMARY KEY CHECK(digest ~ '^[0-9a-f]{64}$'), size bigint NOT NULL CHECK(size >= 0 AND size <= 67108864))`); err != nil {
			return errors.New("metadata migration failed")
		}
	}
	if version < 2 {
		if _, err = tx.ExecContext(ctx, `ALTER TABLE readmit_hub_artifacts ADD COLUMN retained_at timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP`); err != nil {
			return errors.New("metadata migration failed")
		}
	}
	if version < 3 {
		if _, err = tx.ExecContext(ctx, `ALTER TABLE readmit_hub_schema ADD COLUMN team_enabled boolean NOT NULL DEFAULT false; CREATE TABLE readmit_hub_project_artifacts (project text NOT NULL CHECK(project ~ '^[a-z0-9-]{1,64}$'), digest text NOT NULL REFERENCES readmit_hub_artifacts(digest), PRIMARY KEY(project,digest))`); err != nil {
			return errors.New("project metadata migration failed")
		}
	}
	if version < 4 {
		if _, err = tx.ExecContext(ctx, `CREATE TABLE readmit_hub_reviews(project text NOT NULL, sequence integer NOT NULL, id text NOT NULL, document text NOT NULL, PRIMARY KEY(project,sequence), UNIQUE(project,id))`); err != nil {
			return errors.New("review metadata migration failed")
		}
	}
	if version < 5 {
		if _, err = tx.ExecContext(ctx, `CREATE TABLE readmit_hub_lifecycle(project text NOT NULL, sequence integer NOT NULL, id text NOT NULL, document text NOT NULL, PRIMARY KEY(project,sequence), UNIQUE(project,id))`); err != nil {
			return err
		}
	}
	if _, err = tx.ExecContext(ctx, "UPDATE readmit_hub_schema SET version=$1", schemaVersion); err != nil {
		return errors.New("schema version update failed")
	}
	return tx.Commit()
}

func (s *Store) Ready(ctx context.Context) error {
	var version int
	if err := s.db.QueryRowContext(ctx, "SELECT version FROM readmit_hub_schema WHERE singleton").Scan(&version); err != nil || version != schemaVersion {
		return errors.New("metadata schema not ready")
	}
	// Probe the lease's own connection: losing it invalidates exclusivity.
	if err := s.lease.PingContext(ctx); err != nil {
		return errors.New("database lease lost")
	}
	probe := ".health-" + rand.Text()
	f, err := s.root.OpenFile(probe, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return errors.New("artifact storage not writable")
	}
	err = f.Close()
	removeErr := s.root.Remove(probe)
	if err != nil || removeErr != nil {
		return errors.New("artifact storage not writable")
	}
	return nil
}

func validDigest(d string) bool {
	b, e := hex.DecodeString(d)
	return e == nil && len(b) == 32 && hex.EncodeToString(b) == d
}

// Put verifies every uploaded byte before publishing an immutable content object.
// A crash between publication and catalogue commit can leave an unreferenced
// object; a retry verifies and adopts it. Unreferenced objects are never served.
func (s *Store) Put(ctx context.Context, d string, r io.Reader) error {
	if !validDigest(d) {
		return ErrIntegrity
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.Ready(ctx); err != nil {
		return err
	}
	name, n, err := s.stage(ctx, d, r)
	if err != nil {
		return err
	}
	defer s.root.Remove(name)
	var used int64
	var count int
	if err = s.db.QueryRowContext(ctx, "SELECT COALESCE(SUM(size),0),count(*) FROM readmit_hub_artifacts").Scan(&used, &count); err != nil {
		return errors.New("metadata unavailable")
	}
	var existing int64
	err = s.db.QueryRowContext(ctx, "SELECT size FROM readmit_hub_artifacts WHERE digest=$1", d).Scan(&existing)
	if err == nil {
		return s.verify(d, existing)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return errors.New("metadata unavailable")
	}
	if n > s.config.MaxBytes-used || count >= 65536 {
		return ErrLimit
	}
	if err = s.publish(name, d, n); err != nil {
		return err
	}
	if _, err = s.db.ExecContext(ctx, "INSERT INTO readmit_hub_artifacts(digest,size) VALUES($1,$2)", d, n); err != nil {
		return errors.New("artifact catalogue commit failed; retry is safe")
	}
	return nil
}

// stage owns all incomplete bytes. Only a synchronized, verified object can be
// published at a digest pathname, for both uploads and recovery.
func (s *Store) stage(ctx context.Context, d string, r io.Reader) (name string, n int64, err error) {
	name = ".upload-" + rand.Text()
	f, err := s.root.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return "", 0, errors.New("storage unavailable")
	}
	defer f.Close()
	defer func() {
		if err != nil {
			s.root.Remove(name)
		}
	}()
	h := sha256.New()
	n, err = io.Copy(io.MultiWriter(f, h), io.LimitReader(&contextReader{ctx, r}, MaxArtifactBytes+1))
	if err != nil {
		return name, n, errors.New("transfer incomplete")
	}
	if n > MaxArtifactBytes {
		return name, n, ErrLimit
	}
	if hex.EncodeToString(h.Sum(nil)) != d {
		return name, n, ErrIntegrity
	}
	if err = ctx.Err(); err != nil {
		return name, n, err
	}
	if err = f.Sync(); err != nil {
		return name, n, errors.New("transfer persistence failed")
	}
	err = f.Close()
	return name, n, err
}
func (s *Store) publish(name, d string, n int64) error {
	// Link is no-replace: no incomplete write can occupy a final digest path.
	if err := s.root.Link(name, d); err != nil {
		if !errors.Is(err, os.ErrExist) {
			return errors.New("artifact publication failed")
		}
		if err = s.verify(d, n); err != nil {
			return err
		}
	}
	return syncRoot(s.root)
}

type contextReader struct {
	ctx context.Context
	r   io.Reader
}

func (r *contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.r.Read(p)
}

func (s *Store) verify(d string, size int64) error {
	f, err := s.root.Open(d)
	if err != nil {
		return ErrIntegrity
	}
	defer f.Close()
	info, err := s.root.Lstat(d)
	if err != nil || !info.Mode().IsRegular() || info.Size() != size {
		return ErrIntegrity
	}
	h := sha256.New()
	n, err := io.Copy(h, io.LimitReader(f, MaxArtifactBytes+1))
	if err != nil || n != size || hex.EncodeToString(h.Sum(nil)) != d {
		return ErrIntegrity
	}
	return nil
}

// Get checks integrity before returning bytes; callers never stream a corrupt prefix.
func (s *Store) Get(ctx context.Context, d string) ([]byte, error) {
	if !validDigest(d) {
		return nil, ErrMissing
	}
	if err := s.lease.PingContext(ctx); err != nil {
		return nil, errors.New("database lease lost")
	}
	var size int64
	if err := s.db.QueryRowContext(ctx, "SELECT size FROM readmit_hub_artifacts WHERE digest=$1", d).Scan(&size); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrMissing
		}
		return nil, errors.New("metadata unavailable")
	}
	if err := s.verify(d, size); err != nil {
		return nil, err
	}
	f, err := s.root.Open(d)
	if err != nil {
		return nil, ErrIntegrity
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(&contextReader{ctx, f}, MaxArtifactBytes+1))
	if err != nil {
		return nil, ErrIntegrity
	}
	if fmt.Sprintf("%x", sha256.Sum256(data)) != d || int64(len(data)) != size {
		return nil, ErrIntegrity
	}
	return data, nil
}
