package admin

import (
	"errors"
	"net"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"
)

func proxyOptions(opts Options) (string, string, []*net.IPNet, error) {
	if opts.PublicURL == "" {
		if len(opts.TrustedProxies) != 0 {
			return "", "", nil, errors.New("trusted proxies require an explicit public URL")
		}
		return opts.Origin, "", nil, nil
	}
	u, err := url.Parse(opts.PublicURL)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Hostname() == "" || u.RawQuery != "" || u.Fragment != "" || u.RawPath != "" || strings.ContainsAny(u.Host, "\\\r\n\t ") {
		return "", "", nil, errors.New("admin public URL must be an explicit HTTPS URL without credentials, query or fragment")
	}
	prefix := strings.TrimSuffix(u.Path, "/")
	if prefix != "" && (path.Clean(prefix) != prefix || strings.ContainsAny(prefix, "%\\") || strings.Contains(prefix, "//")) {
		return "", "", nil, errors.New("invalid admin public path prefix")
	}
	if len(opts.TrustedProxies) == 0 || len(opts.TrustedProxies) > 16 {
		return "", "", nil, errors.New("admin public URL requires explicit trusted proxy CIDRs")
	}
	proxies := make([]*net.IPNet, 0, len(opts.TrustedProxies))
	for _, value := range opts.TrustedProxies {
		_, network, err := net.ParseCIDR(value)
		if err != nil {
			return "", "", nil, errors.New("invalid trusted proxy CIDR")
		}
		ones, _ := network.Mask.Size()
		if ones == 0 {
			return "", "", nil, errors.New("wildcard proxy trust is not permitted")
		}
		proxies = append(proxies, network)
	}
	return u.Scheme + "://" + u.Host, prefix, proxies, nil
}

// The public identity is configuration, never Forwarded/X-Forwarded-* input.
// Reverse proxies must preserve Host and the configured prefix. The actual
// connection peer, not X-Forwarded-For, is checked against the trust list.
func (s *Server) allowedBoundary(r *http.Request) bool {
	peer, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return false
	}
	ip := net.ParseIP(peer)
	if ip == nil {
		return false
	}
	allowed := ip.IsLoopback() && len(s.trustedProxies) == 0
	for _, network := range s.trustedProxies {
		if network.Contains(ip) {
			allowed = true
			break
		}
	}
	origin, err := url.Parse(s.origin)
	return err == nil && allowed && r.Host == origin.Host && (r.Header.Get("Origin") == "" || r.Header.Get("Origin") == s.origin) && r.Header.Get("Sec-Fetch-Site") != "cross-site"
}

func (s *Server) mountPath(w http.ResponseWriter, r *http.Request) bool {
	// Reject ambiguous escaped separators and normalized traversal before ServeMux
	// can redirect it outside the configured mount.
	if r.URL.RawPath != "" || strings.ContainsAny(r.URL.Path, "\\\x00") || path.Clean(r.URL.Path) != strings.TrimSuffix(r.URL.Path, "/") && r.URL.Path != "/" {
		errorJSON(w, 400, "invalid_request")
		return false
	}
	if s.basePath == "" {
		return true
	}
	if r.URL.Path == s.basePath {
		http.Redirect(w, r, s.basePath+"/", http.StatusPermanentRedirect)
		return false
	}
	if !strings.HasPrefix(r.URL.Path, s.basePath+"/") {
		http.NotFound(w, r)
		return false
	}
	r.URL.Path = strings.TrimPrefix(r.URL.Path, s.basePath)
	return true
}

type prefixCookies struct {
	http.ResponseWriter
	prefix  string
	written bool
}

func (w *prefixCookies) WriteHeader(status int) {
	if w.written {
		return
	}
	w.written = true
	values := append([]string(nil), w.Header().Values("Set-Cookie")...)
	w.Header().Del("Set-Cookie")
	for _, value := range values {
		if c, err := http.ParseSetCookie(value); err == nil {
			c.Path = w.prefix + c.Path
			w.Header().Add("Set-Cookie", c.String())
		}
	}
	w.ResponseWriter.WriteHeader(status)
}
func (w *prefixCookies) Write(body []byte) (int, error) {
	if !w.written {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(body)
}
func requestTimeout(r *http.Request) time.Duration {
	if r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/api/admin/v1/actions/") {
		return 5 * time.Minute
	}
	return 5 * time.Second
}
