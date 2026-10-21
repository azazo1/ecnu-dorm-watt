package monitor

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/azazo1/ecnu-dorm-watt/internal/ecnu"
	"github.com/azazo1/ecnu-dorm-watt/internal/protocol"
	"github.com/azazo1/ecnu-dorm-watt/internal/store"
)

// poll 执行一轮采集并返回下一轮的计划延迟.
func (m *Monitor) poll(ctx context.Context) time.Duration {
	room, session, err := m.loadInputs(ctx)
	if err != nil {
		if ctx.Err() != nil {
			return m.pollInterval()
		}
		return m.fail(fmt.Errorf("读取活动房间与会话失败: %w", err), protocol.CodeStorageError)
	}
	if room == nil {
		m.transition(StateIdle)
		m.mu.Lock()
		m.st.lastError = nil
		m.st.failures = 0
		m.mu.Unlock()
		delay := m.pollInterval()
		m.planNext(delay)
		m.log.Debug("没有活动房间, 暂停采集")
		return delay
	}
	if !sessionUsable(session) {
		m.transition(StateNoSession)
		delay := m.sessionDelay()
		m.planNext(delay)
		m.log.Debug("没有可用会话, 暂停采集", "room_no", room.RoomNo)
		return delay
	}

	// 复制凭据后即可释放存储层资源, 上游请求不持有任何锁.
	target := ecnu.Room{
		RoomNo:  room.RoomNo,
		Elcarea: room.Elcarea,
		Elcbuis: room.Elcbuis,
	}
	credentials := ecnu.Session{
		JSESSIONID: session.JSESSIONID,
		Cookie:     session.Cookie,
		CSRFToken:  session.CSRFToken,
	}

	m.setAttempt(m.now())
	m.log.Debug("开始查询上游电量", "room_no", target.RoomNo)
	degree, queryErr := m.client.QueryDegree(ctx, target, credentials)
	if ctx.Err() != nil {
		return m.pollInterval()
	}
	if queryErr != nil {
		if errors.Is(queryErr, ecnu.ErrNotLoggedIn) {
			return m.handleSessionInvalid(ctx, queryErr)
		}
		return m.fail(queryErr, errorCode(queryErr))
	}
	return m.handleSuccess(ctx, room, session, degree)
}

// loadInputs 读取活动房间与会话, 只返回副本.
func (m *Monitor) loadInputs(ctx context.Context) (*store.Room, *store.Session, error) {
	room, err := m.db.ActiveRoom(ctx)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, nil, nil
		}
		return nil, nil, err
	}
	if room == nil {
		return nil, nil, nil
	}
	roomCopy := *room

	session, err := m.db.GetSession(ctx)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return &roomCopy, nil, nil
		}
		return nil, nil, err
	}
	if session == nil {
		return &roomCopy, nil, nil
	}
	sessionCopy := *session
	return &roomCopy, &sessionCopy, nil
}

func (m *Monitor) handleSuccess(ctx context.Context, room *store.Room, session *store.Session, degree float64) time.Duration {
	now := m.now()
	m.transition(StateActive)

	m.mu.Lock()
	m.st.failures = 0
	m.st.lastSuccess = &now
	m.st.lastDegree = &degree
	m.st.lastError = nil
	m.mu.Unlock()

	if !strings.EqualFold(strings.TrimSpace(session.State), "ok") {
		if err := m.db.SetSessionState(ctx, "ok", now); err != nil {
			m.log.Warn("更新会话状态失败", "err", err)
		}
	}

	m.log.Debug("上游查询成功", "room_no", room.RoomNo, "degree", degree)
	m.maybeRecordSample(ctx, room, degree, now)

	delay := jitterAround(m.pollInterval())
	m.planNext(delay)
	return delay
}

// maybeRecordSample 在度数变化达到阈值时写入样本.
func (m *Monitor) maybeRecordSample(ctx context.Context, room *store.Room, degree float64, now time.Time) {
	threshold := m.threshold()
	last, err := m.db.LastSample(ctx, room.ID)
	switch {
	case err != nil && !errors.Is(err, store.ErrNotFound):
		cause := fmt.Errorf("读取上一条样本失败: %w", err)
		m.setLastError(protocol.CodeStorageError, cause)
		m.log.Warn("读取上一条样本失败", "room_no", room.RoomNo, "err", err)
		return
	case err == nil && last != nil && math.Abs(last.Degree-degree) < threshold:
		m.log.Debug("度数变化未达阈值, 跳过采样", "room_no", room.RoomNo, "degree", degree, "last_degree", last.Degree)
		return
	}

	recordedAt := now.UTC().Truncate(time.Second)
	rounded := roundDegree(degree)
	inserted, err := m.db.InsertSample(ctx, room.ID, recordedAt, rounded)
	if err != nil {
		cause := fmt.Errorf("写入样本失败: %w", err)
		m.setLastError(protocol.CodeStorageError, cause)
		m.log.Warn("写入样本失败", "room_no", room.RoomNo, "err", err)
		return
	}
	if !inserted {
		m.log.Debug("样本已存在, 视为已记录", "room_no", room.RoomNo, "degree", rounded)
		return
	}
	m.log.Info("记录样本", "room_no", room.RoomNo, "degree", rounded)
}

// handleSessionInvalid 处理会话失效: 转入 no_session 并降低探测频率.
func (m *Monitor) handleSessionInvalid(ctx context.Context, cause error) time.Duration {
	m.transition(StateNoSession)

	m.mu.Lock()
	m.st.failures++
	m.st.lastError = &protocol.ErrorInfo{
		Code:    protocol.CodeECNUNotLoggedIn,
		Message: truncateMessage(cause.Error()),
	}
	m.mu.Unlock()

	if err := m.db.SetSessionState(ctx, "invalid", m.now()); err != nil {
		m.log.Warn("标记会话失效失败", "err", err)
	}
	delay := m.sessionDelay()
	m.planNext(delay)
	m.log.Warn("ECNU 会话失效, 降低探测频率", "retry_in", delay, "err", cause)
	return delay
}

// fail 记录一次失败并计算退避延迟.
func (m *Monitor) fail(cause error, code string) time.Duration {
	m.transition(StateDegraded)

	m.mu.Lock()
	m.st.failures++
	failures := m.st.failures
	m.st.lastError = &protocol.ErrorInfo{
		Code:    code,
		Message: truncateMessage(cause.Error()),
	}
	m.mu.Unlock()

	delay := m.backoff(failures)
	m.planNext(delay)
	m.log.Warn("采集失败", "code", code, "failures", failures, "retry_in", delay, "err", cause)
	return delay
}

func sessionUsable(session *store.Session) bool {
	if session == nil {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(session.State)) {
	case "invalid", "missing":
		return false
	}
	return strings.TrimSpace(session.JSESSIONID) != "" &&
		strings.TrimSpace(session.Cookie) != "" &&
		strings.TrimSpace(session.CSRFToken) != ""
}

func errorCode(err error) string {
	switch {
	case errors.Is(err, ecnu.ErrNotLoggedIn):
		return protocol.CodeECNUNotLoggedIn
	case errors.Is(err, ecnu.ErrNoDegree):
		return protocol.CodeECNUNoDegree
	case errors.Is(err, ecnu.ErrCatalogIncomplete):
		return protocol.CodeCatalogIncomplete
	default:
		return protocol.CodeECNUUpstream
	}
}

func roundDegree(degree float64) float64 {
	return math.Round(degree*100) / 100
}
