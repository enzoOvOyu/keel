package platform

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"errors"
	"fmt"
	"github.com/enzoOvOyu/keel/ent"
	"github.com/enzoOvOyu/keel/internal/bmc"
	"github.com/enzoOvOyu/keel/internal/fabricboot"
	"github.com/enzoOvOyu/keel/internal/ldapauth"
	"github.com/enzoOvOyu/keel/internal/mediamanifest"
	"github.com/enzoOvOyu/keel/internal/mediasource"
	"github.com/enzoOvOyu/keel/internal/oidcauth"
	"github.com/enzoOvOyu/keel/internal/taskrunner"
	_ "github.com/jackc/pgx/v5/stdlib"
	"io"
	"time"
)

type MediaObjectStore interface {
	Put(context.Context, string, io.Reader, int64) (string, error)
	Open(context.Context, string, string) (io.ReadCloser, error)
}

type Store struct {
	TopologyFirmwareMockEnabled    bool
	MonitorEnabled                 bool
	PrecollectEnabled              bool
	WiringEnabled                  bool
	BMCInventoryEnabled            bool
	BMCInventory                   *bmc.Suite
	FabricIssueVerificationEnabled bool
	ArtifactTransfersEnabled       bool
	TaskCacheInventoryEnabled      bool
	AgentNetworkInventoryEnabled   bool
	SSHScriptsEnabled              bool
	AgentScriptsEnabled            bool
	// AgentTasksEnabled admits HTTP read-only tasks; no task transport or CA is
	// loaded by this switch. Dispatch remains exclusively worker-owned.
	AgentTasksEnabled       bool
	ProvisionPrepareEnabled bool
	// Installation admission is distinct from source preparation and defaults
	// off. It does not activate any boot listener or permit disk writes.
	ProvisionInstallEnabled bool
	ProvisionImagePublicKey ed25519.PublicKey
	OIDC                    *oidcauth.Service
	LDAP                    *ldapauth.Service
	SSHRunner               *taskrunner.SSH
	MediaObjects            MediaObjectStore
	MediaSources            *mediasource.Fetcher
	MediaManifestSigner     *mediamanifest.Signer
	FabricProbes            *fabricboot.ProbeSuite
	DB                      *sql.DB
	Ent                     *ent.Client
	passwordSlots           chan struct{}
	dummyHash               string
	dsn                     string
	credentialDB            *sql.DB
}

func Open(ctx context.Context, dsn string) (*Store, error) {
	db, e := sql.Open("pgx", dsn)
	if e != nil {
		return nil, errors.New("invalid database configuration")
	}
	db.SetMaxOpenConns(16)
	db.SetMaxIdleConns(4)
	db.SetConnMaxLifetime(30 * time.Minute)
	if e = db.PingContext(ctx); e != nil {
		db.Close()
		return nil, errors.New("database connection unavailable")
	}
	h, e := HashPassword("keel-dummy-password-not-an-account")
	if e != nil {
		db.Close()
		return nil, e
	}
	return &Store{DB: db, Ent: ent.NewClient(ent.Driver(entsql.OpenDB(dialect.Postgres, db))), passwordSlots: make(chan struct{}, 2), dummyHash: h, dsn: dsn}, nil
}
func (s *Store) Close() error {
	if s.BMCInventory != nil {
		s.BMCInventory.Close()
	}
	if s.SSHRunner != nil {
		s.SSHRunner.Close()
	}
	if s.credentialDB != nil {
		s.credentialDB.Close()
	}
	return s.Ent.Close()
}
func RandomID() string {
	var b [16]byte
	if _, e := rand.Read(b[:]); e != nil {
		panic("cryptographic randomness unavailable")
	}
	return hex.EncodeToString(b[:])
}
func randomToken() (string, error) {
	var b [32]byte
	_, e := rand.Read(b[:])
	return hex.EncodeToString(b[:]), e
}
func digest(v string) string        { h := sha256.Sum256([]byte(v)); return hex.EncodeToString(h[:]) }
func TokenHash(token string) string { return digest("keel-session-v1:" + token) }
func CSRFToken(token string) string { return digest("keel-csrf-v1:" + token) }
func validToken(token string) bool {
	if len(token) != 64 {
		return false
	}
	_, e := hex.DecodeString(token)
	return e == nil
}

type Error struct {
	Details       []string
	Status        int
	Code, Message string
	RetryAfter    int
}

func (e *Error) Error() string { return e.Code }
func apiError(status int, code, message string) *Error {
	return &Error{Status: status, Code: code, Message: message}
}

var ErrUnauthenticated = apiError(401, "UNAUTHENTICATED", "Authentication required")
var ErrForbidden = apiError(403, "FORBIDDEN", "Permission denied")
var ErrCredentials = apiError(401, "INVALID_CREDENTIALS", "账号或凭据不正确")
var ErrNotFound = apiError(404, "NOT_FOUND", "Project not found")

func dbError(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("database operation failed: %w", err)
}
