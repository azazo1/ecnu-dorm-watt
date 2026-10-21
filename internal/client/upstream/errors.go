package upstream

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"

	"github.com/azazo1/ecnu-dorm-watt/internal/protocol"
)

// APIError 表示服务端返回了非 2xx 响应.
type APIError struct {
	// Status 是 HTTP 状态码.
	Status int
	// Problem 是解析后的 problem+json, 服务端未返回合法 problem 时字段为零值.
	Problem protocol.Problem
	// Body 是原始响应体摘要, 用于诊断.
	Body string
}

func (e *APIError) Error() string {
	if e.Problem.Code != "" {
		if e.Problem.Detail != "" {
			return fmt.Sprintf("%s: %s", e.Problem.Code, e.Problem.Detail)
		}
		return e.Problem.Code
	}
	if e.Body != "" {
		return fmt.Sprintf("服务端返回 HTTP %d: %s", e.Status, e.Body)
	}
	return fmt.Sprintf("服务端返回 HTTP %d", e.Status)
}

// Code 返回 problem 中的业务错误码.
func (e *APIError) Code() string { return e.Problem.Code }

// TransportError 表示请求没有拿到响应, 例如 DNS, 连接或 TLS 失败.
type TransportError struct {
	// Op 是操作描述, 例如 GET /v1/status.
	Op string
	// Err 是底层错误.
	Err error
	// TLS 表示错误发生在 TLS 握手或证书校验阶段.
	TLS bool
}

func (e *TransportError) Error() string {
	if e.TLS {
		return fmt.Sprintf("%s 失败: TLS 校验或握手错误: %v", e.Op, e.Err)
	}
	return fmt.Sprintf("%s 失败: %v", e.Op, e.Err)
}

func (e *TransportError) Unwrap() error { return e.Err }

// IsTLS 判断错误是否为 TLS 相关问题.
func IsTLS(err error) bool {
	var te *TransportError
	if errors.As(err, &te) {
		return te.TLS
	}
	return isTLSError(err)
}

func isTLSError(err error) bool {
	if err == nil {
		return false
	}
	var (
		certVerify   *tls.CertificateVerificationError
		recordHeader tls.RecordHeaderError
		unknownCA    x509.UnknownAuthorityError
		hostname     x509.HostnameError
		invalidCert  x509.CertificateInvalidError
		opErr        *net.OpError
	)
	if errors.As(err, &certVerify) || errors.As(err, &recordHeader) ||
		errors.As(err, &unknownCA) || errors.As(err, &hostname) || errors.As(err, &invalidCert) {
		return true
	}
	if errors.As(err, &opErr) && opErr.Op == "remote error" {
		return true
	}
	return false
}

// IsAPIError 判断错误是否由服务端响应产生, 并返回该错误.
func IsAPIError(err error) (*APIError, bool) {
	var ae *APIError
	if errors.As(err, &ae) {
		return ae, true
	}
	return nil, false
}

// IsCode 判断错误是否为指定业务错误码.
func IsCode(err error, code string) bool {
	ae, ok := IsAPIError(err)
	return ok && ae.Problem.Code == code
}

// IsUnauthorized 判断错误是否为 401.
func IsUnauthorized(err error) bool {
	ae, ok := IsAPIError(err)
	return ok && ae.Status == 401
}

// IsForbidden 判断错误是否为 403.
func IsForbidden(err error) bool {
	ae, ok := IsAPIError(err)
	return ok && ae.Status == 403
}

// IsTransport 判断错误是否为传输层错误.
func IsTransport(err error) bool {
	var te *TransportError
	return errors.As(err, &te)
}
