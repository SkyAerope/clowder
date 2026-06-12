package proxy

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net"
	"net/http"
	"os"
	"strings"
	"syscall"
	"time"

	"github.com/SkyAerope/clowder/internal/rules"
)

// classifyTransport 把 net/http 错误归到设计定义的传输错误类别。
func classifyTransport(err error) string {
	if err == nil {
		return ""
	}
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return rules.TransportDNSFailure
	}
	if errors.Is(err, syscall.ECONNREFUSED) {
		return rules.TransportConnRefused
	}
	var certErr *tls.CertificateVerificationError
	var unkAuth x509.UnknownAuthorityError
	var hostErr x509.HostnameError
	var recErr tls.RecordHeaderError
	if errors.As(err, &certErr) || errors.As(err, &unkAuth) || errors.As(err, &hostErr) || errors.As(err, &recErr) {
		return rules.TransportTLSError
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, os.ErrDeadlineExceeded) {
		return rules.TransportConnectTimeout
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return rules.TransportConnectTimeout
	}
	msg := err.Error()
	if strings.Contains(msg, "tls") || strings.Contains(msg, "x509") || strings.Contains(msg, "certificate") {
		return rules.TransportTLSError
	}
	if strings.Contains(msg, "unexpected EOF") || strings.Contains(msg, "connection reset") ||
		errors.Is(err, syscall.ECONNRESET) {
		return rules.TransportStreamCut
	}
	return rules.TransportMalformed
}

// upstreamClient 共享 HTTP client：无总超时（流式响应可以很长），只限连接期。
var upstreamClient = &http.Client{
	Transport: &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           (&net.Dialer{Timeout: 15 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 60 * time.Second,
		MaxIdleConnsPerHost:   16,
		IdleConnTimeout:       90 * time.Second,
	},
}
