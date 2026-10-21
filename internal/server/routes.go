package server

import (
	"net/http"
	"sort"
	"strings"

	"github.com/azazo1/ecnu-dorm-watt/internal/protocol"
)

// routeDef 描述一条路由: 方法, 处理器, scope 与特殊限制.
type routeDef struct {
	method   string
	handler  http.Handler
	scope    string
	public   bool
	upstream bool
}

// routes 组装内部路由表.
func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()

	s.route(mux, "/v1/health", routeDef{
		method:  http.MethodGet,
		handler: http.HandlerFunc(s.handleHealth),
		public:  true,
	})
	s.route(mux, "/v1/whoami", routeDef{
		method:  http.MethodGet,
		handler: http.HandlerFunc(s.handleWhoAmI),
		scope:   scopeAny,
	})
	s.route(mux, "/v1/status", routeDef{
		method:  http.MethodGet,
		handler: http.HandlerFunc(s.handleStatus),
		scope:   scopeRead,
	})
	s.route(mux, "/v1/degree", routeDef{
		method:  http.MethodGet,
		handler: http.HandlerFunc(s.handleDegree),
		scope:   scopeRead,
	})
	s.route(mux, "/v1/records", routeDef{
		method:  http.MethodGet,
		handler: http.HandlerFunc(s.handleRecords),
		scope:   scopeRead,
	})

	s.route(mux, "/v1/rooms",
		routeDef{method: http.MethodGet, handler: http.HandlerFunc(s.handleListRooms), scope: scopeRead},
		routeDef{method: http.MethodPost, handler: http.HandlerFunc(s.handleCreateRoom), scope: scopeWrite},
	)
	s.route(mux, "/v1/rooms/active",
		routeDef{method: http.MethodGet, handler: http.HandlerFunc(s.handleActiveRoom), scope: scopeRead},
		routeDef{method: http.MethodDelete, handler: http.HandlerFunc(s.handleClearActiveRoom), scope: scopeWrite},
	)
	s.route(mux, "/v1/rooms/{id}/active", routeDef{
		method:  http.MethodPut,
		handler: http.HandlerFunc(s.handleSetActiveRoom),
		scope:   scopeWrite,
	})
	s.route(mux, "/v1/rooms/{id}/info", routeDef{
		method:  http.MethodGet,
		handler: http.HandlerFunc(s.handleRoomInfo),
		scope:   scopeRead,
	})

	s.route(mux, "/v1/ecnu/districts", routeDef{
		method:   http.MethodGet,
		handler:  http.HandlerFunc(s.handleDistricts),
		scope:    scopeRead,
		upstream: true,
	})
	s.route(mux, "/v1/ecnu/buildings", routeDef{
		method:   http.MethodGet,
		handler:  http.HandlerFunc(s.handleBuildings),
		scope:    scopeRead,
		upstream: true,
	})
	s.route(mux, "/v1/ecnu/floors", routeDef{
		method:   http.MethodGet,
		handler:  http.HandlerFunc(s.handleFloors),
		scope:    scopeRead,
		upstream: true,
	})
	s.route(mux, "/v1/ecnu/rooms", routeDef{
		method:   http.MethodGet,
		handler:  http.HandlerFunc(s.handleCatalogRooms),
		scope:    scopeRead,
		upstream: true,
	})

	s.route(mux, "/v1/session",
		routeDef{method: http.MethodGet, handler: http.HandlerFunc(s.handleGetSession), scope: scopeRead},
		routeDef{method: http.MethodPut, handler: http.HandlerFunc(s.handlePutSession), scope: scopeWrite},
		routeDef{method: http.MethodDelete, handler: http.HandlerFunc(s.handleDeleteSession), scope: scopeWrite},
	)

	s.route(mux, "/v1/archives",
		routeDef{method: http.MethodGet, handler: http.HandlerFunc(s.handleListArchives), scope: scopeRead},
		routeDef{method: http.MethodPost, handler: http.HandlerFunc(s.handleCreateArchive), scope: scopeWrite},
	)
	s.route(mux, "/v1/archives/{name}",
		routeDef{method: http.MethodGet, handler: http.HandlerFunc(s.handleDownloadArchive), scope: scopeRead},
		routeDef{method: http.MethodDelete, handler: http.HandlerFunc(s.handleDeleteArchive), scope: scopeWrite},
	)

	// 未匹配的 /v1 路径统一返回 problem 格式的 404.
	mux.Handle("/v1/", http.HandlerFunc(s.handleNotFound))
	mux.Handle("/v1", http.HandlerFunc(s.handleNotFound))

	return mux
}

// route 注册一条路径上的多个方法.
func (s *Server) route(mux *http.ServeMux, pattern string, defs ...routeDef) {
	table := make(map[string]http.Handler, len(defs))
	allow := make([]string, 0, len(defs))
	for _, def := range defs {
		h := def.handler
		if def.upstream {
			h = s.withUpstreamLimit(h)
		}
		if !def.public {
			h = s.requireAuth(def.scope, h)
		}
		table[def.method] = h
		allow = append(allow, def.method)
	}
	sort.Strings(allow)
	allowHeader := strings.Join(allow, ", ")
	mux.Handle(pattern, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h, ok := table[r.Method]
		if !ok {
			w.Header().Set("Allow", allowHeader)
			s.writeProblem(w, r, http.StatusMethodNotAllowed, protocol.CodeMethodNotAllowed, "", "该路径不支持 "+r.Method+" 方法")
			return
		}
		h.ServeHTTP(w, r)
	}))
}

// withUpstreamLimit 对会触发上游查询的请求按 token 限速.
func (s *Server) withUpstreamLimit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.takeUpstream(w, r) {
			return
		}
		next.ServeHTTP(w, r)
	})
}

// takeUpstream 消耗上游查询配额, 超出时写出 429 并返回 false.
func (s *Server) takeUpstream(w http.ResponseWriter, r *http.Request) bool {
	info, ok := authFrom(r.Context())
	if !ok {
		return true
	}
	key := "upstream:" + info.name
	if !s.upstream.take(key) {
		s.writeRateLimited(w, r, s.upstream.retryAfter(key), "上游查询频率超过限制, 请稍后重试")
		return false
	}
	return true
}
