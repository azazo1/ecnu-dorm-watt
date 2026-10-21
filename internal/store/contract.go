package store

import (
	"context"
	"errors"
	"time"
)

var (
	ErrNotFound     = errors.New("record not found")
	ErrConflict     = errors.New("record conflict")
	ErrDuplicate    = errors.New("duplicate record")
	ErrArchiveEmpty = errors.New("archive range is empty")
	ErrTokenRevoked = errors.New("token is revoked")
)

type Token struct {
	ID         int64
	Name       string
	Prefix     string
	Hash       string
	Scopes     []string
	CreatedAt  time.Time
	LastUsedAt *time.Time
	RevokedAt  *time.Time
}

type Room struct {
	ID        int64
	RoomNo    string
	Elcarea   int
	Elcbuis   string
	CreatedAt time.Time
}

type RoomStats struct {
	SampleCount  int
	LastSampleAt *time.Time
}

type Sample struct {
	Time   time.Time
	Degree float64
}

type Session struct {
	JSESSIONID    string
	Cookie        string
	CSRFToken     string
	State         string
	UpdatedAt     time.Time
	LastCheckedAt *time.Time
}

type ArchiveMeta struct {
	ID         int64
	RoomID     int64
	Name       string
	StartTime  time.Time
	EndTime    time.Time
	RecordsNum int
	CreatedAt  time.Time
	DeletedAt  *time.Time
}

type Stats struct {
	Samples  int
	Archives int
}

type Options struct {
	Path        string
	BusyTimeout time.Duration
}

// DB 是服务端使用的存储契约.
type DB interface {
	Close() error
	Backup(ctx context.Context, out string) error

	CreateToken(ctx context.Context, name, prefix, hash string, scopes []string, now time.Time) (Token, error)
	ListTokens(ctx context.Context) ([]Token, error)
	RevokeToken(ctx context.Context, name string, now time.Time) error
	RotateToken(ctx context.Context, name, prefix, hash string, scopes []string, now time.Time) (Token, error)
	FindTokenByHash(ctx context.Context, hash string) (*Token, error)
	TouchToken(ctx context.Context, id int64, now time.Time) error

	EnsureRoom(ctx context.Context, roomNo string, elcarea int, elcbuis string, now time.Time) (Room, bool, error)
	ListRooms(ctx context.Context) ([]Room, error)
	GetRoom(ctx context.Context, id int64) (*Room, error)
	ActiveRoom(ctx context.Context) (*Room, error)
	SetActiveRoom(ctx context.Context, id int64) error
	ClearActiveRoom(ctx context.Context) error
	RoomStats(ctx context.Context, id int64) (RoomStats, error)

	InsertSample(ctx context.Context, roomID int64, t time.Time, degree float64) (bool, error)
	LastSample(ctx context.Context, roomID int64) (*Sample, error)
	QuerySamples(ctx context.Context, roomID int64, from, to *time.Time, limit int) ([]Sample, error)
	CountSamples(ctx context.Context, roomID int64, from, to *time.Time) (int, error)
	StreamSamples(ctx context.Context, roomID int64, from, to *time.Time, fn func(Sample) error) error

	SaveSession(ctx context.Context, s Session) error
	GetSession(ctx context.Context) (*Session, error)
	DeleteSession(ctx context.Context) error
	SetSessionState(ctx context.Context, state string, checked time.Time) error

	CreateArchive(ctx context.Context, roomID int64, name string, from, to *time.Time, now time.Time) (ArchiveMeta, error)
	ListArchives(ctx context.Context, roomID *int64, includeDeleted bool) ([]ArchiveMeta, error)
	GetArchive(ctx context.Context, roomID *int64, name string) (*ArchiveMeta, error)
	StreamArchive(ctx context.Context, archiveID int64, fn func(Sample) error) error
	DeleteArchive(ctx context.Context, roomID *int64, name string, now time.Time) error
	PurgeDeleted(ctx context.Context, before time.Time) (int, error)

	Stats(ctx context.Context) (Stats, error)
}

// Open 打开或创建 SQLite 数据库.
func Open(ctx context.Context, opts Options) (DB, error) {
	return openSQLite(ctx, opts)
}
