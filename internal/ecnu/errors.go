// Package ecnu 实现 ECNU 电费查询与目录查询的上游客户端.
//
// 客户端只负责单次请求与协议解析, 不重试, 不缓存, 不持有会话.
// 所有错误都可以用 errors.Is 判断类别, 并实现 Code() 以便映射为对外错误码.
package ecnu

import "github.com/azazo1/ecnu-dorm-watt/internal/protocol"

// kindError 是带稳定错误码的哨兵错误.
// 通过 %w 包装后 errors.Is 仍可匹配, errors.As 可取出 Code().
type kindError struct {
	code string
	msg  string
}

func (e *kindError) Error() string { return e.msg }

// Code 返回对外错误码, 取值见 protocol.Code*.
func (e *kindError) Code() string { return e.code }

var (
	// ErrNotLoggedIn 表示上游会话失效或未登录, 需要用户重新上传凭据.
	// 跳转 SSO, HTML 响应以及非 JSON 响应都归入此类.
	ErrNotLoggedIn = &kindError{code: protocol.CodeECNUNotLoggedIn, msg: "ecnu: 会话失效或未登录"}

	// ErrUpstream 表示上游网络错误, 非 2xx 状态, 响应解析失败或业务错误.
	ErrUpstream = &kindError{code: protocol.CodeECNUUpstream, msg: "ecnu: 上游请求失败"}

	// ErrNoDegree 表示响应中缺少电量字段, 通常意味着上游协议变化.
	ErrNoDegree = &kindError{code: protocol.CodeECNUNoDegree, msg: "ecnu: 上游响应缺少电量字段"}

	// ErrCatalogIncomplete 表示目录数据缺少房间解析所需的条目.
	ErrCatalogIncomplete = &kindError{code: protocol.CodeCatalogIncomplete, msg: "ecnu: 上游目录数据不完整"}
)
