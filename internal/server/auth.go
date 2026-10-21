package server

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/azazo1/ecnu-dorm-watt/internal/config"
	"github.com/azazo1/ecnu-dorm-watt/internal/protocol"
	"github.com/azazo1/ecnu-dorm-watt/internal/store"
)

const (
	// scopeRead 允许读取类接口.
	scopeRead = "read"
	// scopeWrite 隐含 read, 允许修改会话, 房间与归档.
	scopeWrite = "write"
	// scopeAny 表示只要求 token 有效, 不要求具体 scope.
	scopeAny = ""
)

// staticToken 是配置中声明的静态 token.
type staticToken struct {
	name   string
	scopes []string
	sha256 string
}

// authInfo 是通过鉴权的调用方信息.
type authInfo struct {
	name   string
	scopes []string
	static bool
}

// authFailure 描述一次鉴权失败, 携带要返回给客户端的状态码与错误码.
type authFailure struct {
	status int
	code   string
	detail string
}

func (f *authFailure) Error() string { return f.detail }

// buildStaticTokens 规范化静态 token 列表.
func buildStaticTokens(raw []config.StaticToken) []staticToken {
	out := make([]staticToken, 0, len(raw))
	for _, item := range raw {
		name := strings.TrimSpace(item.Name)
		hash := strings.ToLower(strings.TrimSpace(item.SHA256))
		if name == "" || hash == "" {
			continue
		}
		out = append(out, staticToken{
			name:   name,
			scopes: normalizeScopes(item.Scopes),
			sha256: hash,
		})
	}
	return out
}

// normalizeScopes 去重并排序 scope 列表.
func normalizeScopes(scopes []string) []string {
	seen := make(map[string]struct{}, len(scopes))
	out := make([]string, 0, len(scopes))
	for _, scope := range scopes {
		s := strings.ToLower(strings.TrimSpace(scope))
		if s == "" {
			continue
		}
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

// hasScope 判断 token scope 是否满足要求.
func hasScope(scopes []string, needed string) bool {
	if needed == scopeAny {
		return true
	}
	hasRead := false
	hasWrite := false
	for _, scope := range scopes {
		switch scope {
		case scopeRead:
			hasRead = true
		case scopeWrite:
			hasWrite = true
		}
	}
	switch needed {
	case scopeRead:
		return hasRead || hasWrite
	case scopeWrite:
		return hasWrite
	default:
		return false
	}
}

// authFrom 从上下文取出调用方信息.
func authFrom(ctx context.Context) (authInfo, bool) {
	info, ok := ctx.Value(ctxKeyAuth).(authInfo)
	return info, ok
}

// requireAuth 返回带 Bearer 鉴权与按 token 限速的处理器.
//
// scope 为空表示只要求 token 有效; 缺失或无效 token 返回 401, scope 不足返回 403.
// 认证失败按客户端 IP 限速, 超过限制返回 429.
func (s *Server) requireAuth(scope string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := s.clientIP(r)
		if !s.failedAuth.available(ip) {
			s.writeRateLimited(w, r, s.failedAuth.retryAfter(ip), "认证失败次数过多, 请稍后重试")
			return
		}
		info, err := s.authenticate(r)
		if err != nil {
			s.failedAuth.take(ip)
			var failure *authFailure
			if errors.As(err, &failure) {
				if failure.code == protocol.CodeUnauthorized {
					s.logger.Warn("认证失败", "remote_ip", ip, "reason", failure.detail)
				}
				s.writeProblem(w, r, failure.status, failure.code, "", failure.detail)
				return
			}
			s.writeStorageProblem(w, r, err)
			return
		}
		if !hasScope(info.scopes, scope) {
			s.logger.Warn("token 权限不足", "remote_ip", ip, "token_name", info.name, "needed", scope)
			s.writeProblem(w, r, http.StatusForbidden, protocol.CodeForbidden, "", "token 缺少所需的权限范围")
			return
		}
		key := "token:" + info.name
		if !s.requests.take(key) {
			s.writeRateLimited(w, r, s.requests.retryAfter(key), "请求频率超过限制, 请稍后重试")
			return
		}
		if audit, ok := r.Context().Value(ctxKeyAudit).(*requestAudit); ok {
			audit.tokenName = info.name
		}
		ctx := context.WithValue(r.Context(), ctxKeyAuth, info)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// authenticate 解析并校验 Authorization 头.
func (s *Server) authenticate(r *http.Request) (authInfo, error) {
	values := r.Header.Values("Authorization")
	if len(values) != 1 {
		return authInfo{}, unauthorized("缺少或无效的 Bearer token")
	}
	raw := strings.TrimSpace(values[0])
	scheme, token, ok := strings.Cut(raw, " ")
	if !ok || !strings.EqualFold(scheme, "bearer") {
		return authInfo{}, unauthorized("缺少或无效的 Bearer token")
	}
	token = strings.TrimSpace(token)
	if token == "" || strings.ContainsAny(token, " \t\r\n") {
		return authInfo{}, unauthorized("缺少或无效的 Bearer token")
	}

	sum := sha256.Sum256([]byte(token))
	hash := hex.EncodeToString(sum[:])

	dbToken, err := s.db.FindTokenByHash(r.Context(), hash)
	switch {
	case err == nil:
		if dbToken != nil && dbToken.RevokedAt == nil {
			s.touchToken(r.Context(), dbToken.ID)
			return authInfo{
				name:   dbToken.Name,
				scopes: normalizeScopes(dbToken.Scopes),
			}, nil
		}
	case errors.Is(err, store.ErrNotFound), errors.Is(err, store.ErrTokenRevoked), errors.Is(err, store.ErrConflict):
		// 继续尝试静态 token.
	default:
		return authInfo{}, err
	}

	if token, ok := s.lookupStaticToken(hash); ok {
		return token, nil
	}
	return authInfo{}, unauthorized("缺少或无效的 Bearer token")
}

// lookupStaticToken 在配置的静态 token 中查找摘要.
func (s *Server) lookupStaticToken(hash string) (authInfo, bool) {
	for _, item := range s.staticTokens {
		if subtle.ConstantTimeCompare([]byte(item.sha256), []byte(hash)) == 1 {
			return authInfo{name: item.name, scopes: item.scopes, static: true}, true
		}
	}
	return authInfo{}, false
}

// unauthorized 构造 401 鉴权失败.
func unauthorized(detail string) error {
	return &authFailure{status: http.StatusUnauthorized, code: protocol.CodeUnauthorized, detail: detail}
}

// touchToken 更新 token 最近使用时间, 每个 token 每分钟最多写一次库.
func (s *Server) touchToken(ctx context.Context, id int64) {
	if !s.tokenTouch.due(id, time.Now()) {
		return
	}
	if err := s.db.TouchToken(ctx, id, time.Now()); err != nil {
		s.logger.Debug("更新 token 最近使用时间失败", "token_id", id, "error", err)
	}
}

// touchCache 记录 token 最近写库时间.
type touchCache struct {
	mu       sync.Mutex
	interval time.Duration
	last     map[int64]time.Time
}

// newTouchCache 构造节流器.
func newTouchCache(interval time.Duration) *touchCache {
	return &touchCache{interval: interval, last: make(map[int64]time.Time)}
}

// due 判断本次是否应该写库.
func (c *touchCache) due(id int64, now time.Time) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if last, ok := c.last[id]; ok && now.Sub(last) < c.interval {
		return false
	}
	c.last[id] = now
	if len(c.last) > 1024 {
		for key, at := range c.last {
			if now.Sub(at) > c.interval {
				delete(c.last, key)
			}
		}
	}
	return true
}
