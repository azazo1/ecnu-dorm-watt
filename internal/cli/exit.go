package cli

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/azazo1/ecnu-dorm-watt/internal/client/upstream"
	"github.com/azazo1/ecnu-dorm-watt/internal/protocol"
)

// 退出码定义, 与设计文档 06 第 12 节一致.
const (
	ExitOK            = 0
	ExitGeneral       = 1
	ExitUsage         = 2
	ExitAuth          = 3
	ExitNetwork       = 4
	ExitNotConfigured = 5
	ExitServer        = 6
	ExitDaemon        = 7
	ExitInterrupted   = 130
)

// ExitError 携带明确的退出码.
type ExitError struct {
	Code int
	Err  error
}

func (e *ExitError) Error() string {
	if e.Err == nil {
		return fmt.Sprintf("exit %d", e.Code)
	}
	return e.Err.Error()
}

func (e *ExitError) Unwrap() error { return e.Err }

// exitErr 用指定退出码包装错误.
func exitErr(code int, err error) error {
	if err == nil {
		return nil
	}
	return &ExitError{Code: code, Err: err}
}

// silentExit 表示只设置退出码, 不输出错误文本.
type silentExit struct{ Code int }

func (e *silentExit) Error() string { return "" }

// ExitCode 返回退出码.
func (e *silentExit) ExitCode() int { return e.Code }

// usageErrorf 构造参数或配置错误, 退出码为 2.
func usageErrorf(format string, args ...any) error {
	return exitErr(ExitUsage, fmt.Errorf(format, args...))
}

// classifyError 把错误映射为退出码.
func classifyError(err error) int {
	if err == nil {
		return ExitOK
	}
	var coded interface{ ExitCode() int }
	if errors.As(err, &coded) {
		return coded.ExitCode()
	}
	var ee *ExitError
	if errors.As(err, &ee) {
		return ee.Code
	}
	if ae, ok := upstream.IsAPIError(err); ok {
		switch {
		case ae.Status == 401 || ae.Status == 403:
			return ExitAuth
		case ae.Problem.Code == protocol.CodeRoomNotConfigured,
			ae.Problem.Code == protocol.CodeECNUNotLoggedIn:
			return ExitNotConfigured
		case ae.Problem.Code == protocol.CodeECNUUpstream,
			ae.Problem.Code == protocol.CodeECNUNoDegree,
			ae.Problem.Code == protocol.CodeUnavailable,
			ae.Problem.Code == protocol.CodeUpstreamUnreachable:
			return ExitNetwork
		default:
			return ExitServer
		}
	}
	if upstream.IsTransport(err) {
		return ExitNetwork
	}
	if errors.Is(err, context.Canceled) {
		return ExitInterrupted
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return ExitNetwork
	}
	msg := err.Error()
	if strings.Contains(msg, "unknown command") || strings.Contains(msg, "unknown flag") ||
		strings.Contains(msg, "requires at least") || strings.Contains(msg, "accepts at most") ||
		strings.Contains(msg, "accepts ") && strings.Contains(msg, "arg(s)") {
		return ExitUsage
	}
	return ExitGeneral
}

// errorCode 返回 JSON 错误响应中的 code 字段.
func errorCode(err error) string {
	if ae, ok := upstream.IsAPIError(err); ok && ae.Code() != "" {
		return ae.Code()
	}
	var ee *ExitError
	if errors.As(err, &ee) {
		switch ee.Code {
		case ExitUsage:
			return "usage_error"
		case ExitAuth:
			return "unauthorized"
		case ExitNetwork:
			return "network_error"
		case ExitNotConfigured:
			return "not_configured"
		case ExitServer:
			return "server_error"
		case ExitDaemon:
			return "daemon_not_running"
		case ExitInterrupted:
			return "interrupted"
		default:
			return "error"
		}
	}
	if upstream.IsTransport(err) {
		return "network_error"
	}
	return "error"
}
