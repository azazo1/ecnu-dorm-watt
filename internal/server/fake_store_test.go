package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"sort"
	"time"

	"github.com/azazo1/ecnu-dorm-watt/internal/store"
)

// fakeStore 是测试用的最小内存存储实现.
type fakeStore struct {
	tokens         map[string]store.Token
	rooms          map[int64]store.Room
	activeID       int64
	samples        map[int64][]store.Sample
	session        *store.Session
	archives       map[int64]store.ArchiveMeta
	archiveSamples map[int64][]store.Sample
	nextRoomID     int64
	nextArchiveID  int64
	nextTokenID    int64
	panicOnStats   bool
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		tokens:         make(map[string]store.Token),
		rooms:          make(map[int64]store.Room),
		samples:        make(map[int64][]store.Sample),
		archives:       make(map[int64]store.ArchiveMeta),
		archiveSamples: make(map[int64][]store.Sample),
		nextRoomID:     1,
		nextArchiveID:  1,
		nextTokenID:    1,
	}
}

// addToken 注册一个 token, 返回明文.
func (f *fakeStore) addToken(name string, scopes ...string) string {
	raw := "dw_" + name
	sum := sha256.Sum256([]byte(raw))
	f.nextTokenID++
	f.tokens[hex.EncodeToString(sum[:])] = store.Token{
		ID:     f.nextTokenID,
		Name:   name,
		Prefix: raw[:5],
		Hash:   hex.EncodeToString(sum[:]),
		Scopes: scopes,
	}
	return raw
}

// addRoom 注册一个房间.
func (f *fakeStore) addRoom(roomNo string, elcarea int, elcbuis string, createdAt time.Time) store.Room {
	room := store.Room{
		ID:        f.nextRoomID,
		RoomNo:    roomNo,
		Elcarea:   elcarea,
		Elcbuis:   elcbuis,
		CreatedAt: createdAt,
	}
	f.nextRoomID++
	f.rooms[room.ID] = room
	return room
}

// addSample 追加一个样本.
func (f *fakeStore) addSample(roomID int64, at time.Time, degree float64) {
	f.samples[roomID] = append(f.samples[roomID], store.Sample{Time: at, Degree: degree})
}

// addArchive 直接注册一个归档.
func (f *fakeStore) addArchive(meta store.ArchiveMeta, samples []store.Sample) store.ArchiveMeta {
	f.nextArchiveID++
	meta.ID = f.nextArchiveID
	f.archives[meta.ID] = meta
	f.archiveSamples[meta.ID] = samples
	return meta
}

func (f *fakeStore) Close() error { return nil }

// Backup 在测试中不生成快照.
func (f *fakeStore) Backup(ctx context.Context, out string) error { return nil }

func (f *fakeStore) CreateToken(ctx context.Context, name, prefix, hash string, scopes []string, now time.Time) (store.Token, error) {
	return store.Token{}, errors.New("not implemented")
}

func (f *fakeStore) ListTokens(ctx context.Context) ([]store.Token, error) {
	out := make([]store.Token, 0, len(f.tokens))
	for _, token := range f.tokens {
		out = append(out, token)
	}
	return out, nil
}

func (f *fakeStore) RevokeToken(ctx context.Context, name string, now time.Time) error {
	return errors.New("not implemented")
}

func (f *fakeStore) RotateToken(ctx context.Context, name, prefix, hash string, scopes []string, now time.Time) (store.Token, error) {
	return store.Token{}, errors.New("not implemented")
}

func (f *fakeStore) FindTokenByHash(ctx context.Context, hash string) (*store.Token, error) {
	token, ok := f.tokens[hash]
	if !ok {
		return nil, store.ErrNotFound
	}
	if token.RevokedAt != nil {
		return nil, store.ErrTokenRevoked
	}
	return &token, nil
}

func (f *fakeStore) TouchToken(ctx context.Context, id int64, now time.Time) error { return nil }

func (f *fakeStore) EnsureRoom(ctx context.Context, roomNo string, elcarea int, elcbuis string, now time.Time) (store.Room, bool, error) {
	for _, room := range f.rooms {
		if room.RoomNo == roomNo && room.Elcarea == elcarea && room.Elcbuis == elcbuis {
			return room, false, nil
		}
	}
	room := f.addRoom(roomNo, elcarea, elcbuis, now)
	return room, true, nil
}

func (f *fakeStore) ListRooms(ctx context.Context) ([]store.Room, error) {
	out := make([]store.Room, 0, len(f.rooms))
	for _, room := range f.rooms {
		out = append(out, room)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (f *fakeStore) GetRoom(ctx context.Context, id int64) (*store.Room, error) {
	room, ok := f.rooms[id]
	if !ok {
		return nil, store.ErrNotFound
	}
	return &room, nil
}

func (f *fakeStore) ActiveRoom(ctx context.Context) (*store.Room, error) {
	if f.activeID == 0 {
		return nil, store.ErrNotFound
	}
	return f.GetRoom(ctx, f.activeID)
}

func (f *fakeStore) SetActiveRoom(ctx context.Context, id int64) error {
	if _, ok := f.rooms[id]; !ok {
		return store.ErrNotFound
	}
	f.activeID = id
	return nil
}

func (f *fakeStore) ClearActiveRoom(ctx context.Context) error {
	f.activeID = 0
	return nil
}

func (f *fakeStore) RoomStats(ctx context.Context, id int64) (store.RoomStats, error) {
	stats := store.RoomStats{SampleCount: len(f.samples[id])}
	if samples := f.samples[id]; len(samples) > 0 {
		last := samples[len(samples)-1].Time
		stats.LastSampleAt = &last
	}
	return stats, nil
}

func (f *fakeStore) InsertSample(ctx context.Context, roomID int64, t time.Time, degree float64) (bool, error) {
	f.addSample(roomID, t, degree)
	return true, nil
}

func (f *fakeStore) LastSample(ctx context.Context, roomID int64) (*store.Sample, error) {
	samples := f.samples[roomID]
	if len(samples) == 0 {
		return nil, store.ErrNotFound
	}
	last := samples[len(samples)-1]
	return &last, nil
}

// filterSamples 按时间范围过滤, 结果按时间升序.
func filterSamples(samples []store.Sample, from, to *time.Time) []store.Sample {
	out := make([]store.Sample, 0, len(samples))
	for _, sample := range samples {
		if from != nil && sample.Time.Before(*from) {
			continue
		}
		if to != nil && sample.Time.After(*to) {
			continue
		}
		out = append(out, sample)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Time.Before(out[j].Time) })
	return out
}

func (f *fakeStore) QuerySamples(ctx context.Context, roomID int64, from, to *time.Time, limit int) ([]store.Sample, error) {
	out := filterSamples(f.samples[roomID], from, to)
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (f *fakeStore) CountSamples(ctx context.Context, roomID int64, from, to *time.Time) (int, error) {
	return len(filterSamples(f.samples[roomID], from, to)), nil
}

func (f *fakeStore) StreamSamples(ctx context.Context, roomID int64, from, to *time.Time, fn func(store.Sample) error) error {
	for _, sample := range filterSamples(f.samples[roomID], from, to) {
		if err := fn(sample); err != nil {
			return err
		}
	}
	return nil
}

func (f *fakeStore) SaveSession(ctx context.Context, s store.Session) error {
	f.session = &s
	return nil
}

func (f *fakeStore) GetSession(ctx context.Context) (*store.Session, error) {
	if f.session == nil {
		return nil, store.ErrNotFound
	}
	return f.session, nil
}

func (f *fakeStore) DeleteSession(ctx context.Context) error {
	f.session = nil
	return nil
}

func (f *fakeStore) SetSessionState(ctx context.Context, state string, checked time.Time) error {
	if f.session == nil {
		return store.ErrNotFound
	}
	f.session.State = state
	f.session.LastCheckedAt = &checked
	return nil
}

func (f *fakeStore) CreateArchive(ctx context.Context, roomID int64, name string, from, to *time.Time, now time.Time) (store.ArchiveMeta, error) {
	if _, ok := f.rooms[roomID]; !ok {
		return store.ArchiveMeta{}, store.ErrNotFound
	}
	for _, meta := range f.archives {
		if meta.RoomID == roomID && meta.Name == name {
			return store.ArchiveMeta{}, store.ErrDuplicate
		}
	}
	samples := filterSamples(f.samples[roomID], from, to)
	if len(samples) == 0 {
		return store.ArchiveMeta{}, store.ErrArchiveEmpty
	}
	meta := store.ArchiveMeta{
		RoomID:     roomID,
		Name:       name,
		StartTime:  samples[0].Time,
		EndTime:    samples[len(samples)-1].Time,
		RecordsNum: len(samples),
		CreatedAt:  now,
	}
	meta = f.addArchive(meta, samples)
	// 归档后范围内样本从活动记录流中移除.
	remaining := make([]store.Sample, 0, len(f.samples[roomID]))
	for _, sample := range f.samples[roomID] {
		if sample.Time.Before(meta.StartTime) || sample.Time.After(meta.EndTime) {
			remaining = append(remaining, sample)
		}
	}
	f.samples[roomID] = remaining
	return meta, nil
}

func (f *fakeStore) ListArchives(ctx context.Context, roomID *int64, includeDeleted bool) ([]store.ArchiveMeta, error) {
	out := make([]store.ArchiveMeta, 0, len(f.archives))
	for _, meta := range f.archives {
		if roomID != nil && meta.RoomID != *roomID {
			continue
		}
		if !includeDeleted && meta.DeletedAt != nil {
			continue
		}
		out = append(out, meta)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if !out[i].StartTime.Equal(out[j].StartTime) {
			return out[i].StartTime.Before(out[j].StartTime)
		}
		return out[i].Name < out[j].Name
	})
	return out, nil
}

func (f *fakeStore) GetArchive(ctx context.Context, roomID *int64, name string) (*store.ArchiveMeta, error) {
	for _, meta := range f.archives {
		if meta.Name != name || meta.DeletedAt != nil {
			continue
		}
		if roomID != nil && meta.RoomID != *roomID {
			continue
		}
		out := meta
		return &out, nil
	}
	return nil, store.ErrNotFound
}

func (f *fakeStore) StreamArchive(ctx context.Context, archiveID int64, fn func(store.Sample) error) error {
	samples := f.archiveSamples[archiveID]
	for _, sample := range samples {
		if err := fn(sample); err != nil {
			return err
		}
	}
	return nil
}

func (f *fakeStore) DeleteArchive(ctx context.Context, roomID *int64, name string, now time.Time) error {
	for id, meta := range f.archives {
		if meta.Name != name || meta.DeletedAt != nil {
			continue
		}
		if roomID != nil && meta.RoomID != *roomID {
			continue
		}
		deleted := now
		meta.DeletedAt = &deleted
		f.archives[id] = meta
		return nil
	}
	return store.ErrNotFound
}

func (f *fakeStore) PurgeDeleted(ctx context.Context, before time.Time) (int, error) { return 0, nil }

func (f *fakeStore) Stats(ctx context.Context) (store.Stats, error) {
	if f.panicOnStats {
		panic("测试用 panic")
	}
	stats := store.Stats{Archives: len(f.archives)}
	for _, samples := range f.samples {
		stats.Samples += len(samples)
	}
	return stats, nil
}
