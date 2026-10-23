package platform

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"errors"
	"fmt"
	"github.com/enzoOvOyu/keel/ent"
	"github.com/enzoOvOyu/keel/internal/fabricboot"
	_ "github.com/jackc/pgx/v5/stdlib"
	"time"
)

type Store struct {
	FabricProbes  *fabricboot.ProbeSuite
	DB            *sql.DB
	Ent           *ent.Client
	passwordSlots chan struct{}
	dummyHash     string
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
	return &Store{DB: db, Ent: ent.NewClient(ent.Driver(entsql.OpenDB(dialect.Postgres, db))), passwordSlots: make(chan struct{}, 2), dummyHash: h}, nil
}
func (s *Store) Close() error { return s.Ent.Close() }
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
