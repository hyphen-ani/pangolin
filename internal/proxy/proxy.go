package proxy

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
)

// Destination says where one request goes. The gateway builds it per
// request from the matched route and its service.
type Destination struct {
	Target       *url.URL // scheme, host:port and base path of the upstream
	StripPrefix  string   // removed from the request path first; "" = none
	PreserveHost bool     // forward the client's Host instead of the upstream's
}

// Forwarder sends requests upstream and streams responses back.
// It holds no routing state, so one Forwarder serves every route.
type Forwarder struct {
	transport http.RoundTripper
	logger    *slog.Logger
}

func NewForwarder(transport http.RoundTripper, logger *slog.Logger) *Forwarder {
	return &Forwarder{transport: transport, logger: logger}
}

var bufPool = sync.Pool{
	New: func() any {
		b := make([]byte, 32*1024)
		return &b
	},
}

func (f *Forwarder) Forward(w http.ResponseWriter, r *http.Request, dst Destination) {
	ctx := r.Context()

	out := r.Clone(ctx)
	if r.ContentLength == 0 {
		out.Body = nil
	}
	out.Close = false
	out.RequestURI = ""
	rewriteURL(out, dst)

	wantsTrailers := teHasTrailers(r.Header)
	removeHopHeaders(out.Header)
	if wantsTrailers {
		out.Header.Set("Te", "trailers")
	}
	setForwardedHeaders(out, r)

	res, err := f.transport.RoundTrip(out)
	if err != nil {
		f.handleError(w, r, err)
		return
	}
	defer res.Body.Close()

	removeHopHeaders(res.Header)
	copyHeader(w.Header(), res.Header)
	if len(res.Trailer) > 0 {
		names := make([]string, 0, len(res.Trailer))
		for k := range res.Trailer {
			names = append(names, k)
		}
		w.Header().Add("Trailer", strings.Join(names, ", "))
	}
	w.WriteHeader(res.StatusCode)

	if err := copyBody(w, res.Body); err != nil {
		f.logger.Warn("response body copy failed", "path", r.URL.Path, "err", err)
		panic(http.ErrAbortHandler)
	}
	for k, vv := range res.Trailer {
		w.Header()[k] = vv
	}
}

// rewriteURL applies strip_path, joins the service base path, and sets
// the upstream scheme, host and Host header.
func rewriteURL(req *http.Request, dst Destination) {
	t := dst.Target

	if dst.StripPrefix != "" {
		stripPrefix(req.URL, dst.StripPrefix)
	}
	// Stripping "/users" from "/users" leaves "/"; send the base path
	// itself ("/api"), not "/api/", which many frameworks treat differently.
	if dst.StripPrefix != "" && req.URL.Path == "/" && t.Path != "" {
		req.URL.Path, req.URL.RawPath = t.Path, t.RawPath
	} else {
		req.URL.Path, req.URL.RawPath = joinURLPath(t, req.URL)
	}

	req.URL.Scheme = t.Scheme
	req.URL.Host = t.Host
	switch {
	case t.RawQuery == "":
	case req.URL.RawQuery == "":
		req.URL.RawQuery = t.RawQuery
	default:
		req.URL.RawQuery = t.RawQuery + "&" + req.URL.RawQuery
	}

	if !dst.PreserveHost {
		req.Host = "" // Transport then sends the upstream's host
	}
}

// stripPrefix removes a matched route prefix: "/users" on "/users/42"
// leaves "/42", and on "/users" leaves "/".
func stripPrefix(u *url.URL, prefix string) {
	u.Path = ensureLeadingSlash(strings.TrimPrefix(u.Path, prefix))
	if u.RawPath != "" {
		if strings.HasPrefix(u.RawPath, prefix) {
			u.RawPath = ensureLeadingSlash(strings.TrimPrefix(u.RawPath, prefix))
		} else {
			u.RawPath = "" // fall back to Path; it's always correct, just re-encoded
		}
	}
}

func ensureLeadingSlash(p string) string {
	if strings.HasPrefix(p, "/") {
		return p
	}
	return "/" + p
}

func setForwardedHeaders(out, in *http.Request) {
	if clientIP, _, err := net.SplitHostPort(in.RemoteAddr); err == nil {
		// SECURITY: trusted-proxies handling arrives in Phase 2.
		if prior := out.Header.Values("X-Forwarded-For"); len(prior) > 0 {
			clientIP = strings.Join(prior, ", ") + ", " + clientIP
		}
		out.Header.Set("X-Forwarded-For", clientIP)
	}
	proto := "http"
	if in.TLS != nil {
		proto = "https"
	}
	out.Header.Set("X-Forwarded-Proto", proto)
	out.Header.Set("X-Forwarded-Host", in.Host)
}

func (f *Forwarder) handleError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, context.Canceled):
		f.logger.Debug("client canceled request", "path", r.URL.Path)
		w.WriteHeader(499)
	case errors.Is(err, context.DeadlineExceeded) || isTimeout(err):
		f.logger.Warn("upstream timeout", "path", r.URL.Path, "err", err)
		WriteError(w, http.StatusGatewayTimeout, "upstream timed out")
	default:
		f.logger.Warn("upstream error", "path", r.URL.Path, "err", err)
		WriteError(w, http.StatusBadGateway, "upstream unavailable")
	}
}

func isTimeout(err error) bool {
	var ne net.Error
	return errors.As(err, &ne) && ne.Timeout()
}

func copyHeader(dst, src http.Header) {
	for k, vv := range src {
		for _, v := range vv {
			dst.Add(k, v)
		}
	}
}

func copyBody(dst io.Writer, src io.Reader) error {
	bp := bufPool.Get().(*[]byte)
	defer bufPool.Put(bp)
	_, err := io.CopyBuffer(dst, src, *bp)
	return err
}

func joinURLPath(a, b *url.URL) (path, rawPath string) {
	if a.RawPath == "" && b.RawPath == "" {
		return singleJoiningSlash(a.Path, b.Path), ""
	}
	apath, bpath := a.EscapedPath(), b.EscapedPath()
	aslash, bslash := strings.HasSuffix(apath, "/"), strings.HasPrefix(bpath, "/")
	switch {
	case aslash && bslash:
		return a.Path + b.Path[1:], apath + bpath[1:]
	case !aslash && !bslash:
		return a.Path + "/" + b.Path, apath + "/" + bpath
	}
	return a.Path + b.Path, apath + bpath
}

func singleJoiningSlash(a, b string) string {
	aslash, bslash := strings.HasSuffix(a, "/"), strings.HasPrefix(b, "/")
	switch {
	case aslash && bslash:
		return a + b[1:]
	case !aslash && !bslash:
		return a + "/" + b
	}
	return a + b
}
