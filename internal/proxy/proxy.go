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

type Handler struct {
	target    *url.URL
	transport http.RoundTripper
	logger    *slog.Logger
}

/** Helpers Functions **/

/*
* rewriteURL points the outbound request at the upstream, joining
the upstream's base path with the client's path *
*/
func (h *Handler) rewriteURL(req *http.Request) {
	t := h.target
	req.URL.Scheme = t.Scheme
	req.URL.Host = t.Host
	req.URL.Path, req.URL.RawPath = joinURLPath(t, req.URL)

	switch {
	case t.RawQuery == "":
	case req.URL.RawQuery == "":
		req.URL.RawQuery = t.RawQuery
	default:
		req.URL.RawQuery = t.RawQuery + "&" + req.URL.RawQuery
	}

	req.Host = ""

}

/** setForwardedHeaders tells the upstream who the real client is **/
func setForwardedHeaders(out, in *http.Request) {
	if clientIP, _, err := net.SplitHostPort(in.RemoteAddr); err == nil {
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

func (h *Handler) handleError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, context.Canceled):
		h.logger.Debug("client canceled request", "path", r.URL.Path)
		w.WriteHeader(499)
	case errors.Is(err, context.DeadlineExceeded) || isTimeout(err):
		h.logger.Warn("upstream timeout", "path", r.URL.Path, "err", err)
		writeError(w, http.StatusGatewayTimeout, "upstream timed out")
	default:
		h.logger.Warn("upstream error", "path", r.URL.Path, "err", err)
		writeError(w, http.StatusBadGateway, "upstream unavailable")
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

func joinURLPath(a, b *url.URL) (path, rawpath string) {
	if a.RawPath == "" && b.RawPath == "" {
		return singleJoiningSlash(a.Path, b.Path), ""
	}
	apath, bpath := a.EscapedPath(), b.EscapedPath()
	aslash, bslash := strings.HasSuffix(apath, "/"), strings.HasPrefix(bpath, "/")
	switch {
	case aslash && bslash:
		return a.Path + b.Path[1:], apath + b.Path[1:]
	case !aslash && !bslash:
		return a.Path + "/" + b.Path, apath + "/" + b.Path
	}

	return a.Path + b.Path, apath + b.Path
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

func New(target *url.URL, transport http.RoundTripper, logger *slog.Logger) *Handler {
	return &Handler{
		target:    target,
		transport: transport,
		logger:    logger,
	}
}

var bufPool = sync.Pool{
	New: func() any {
		b := make([]byte, 32*1024)
		return &b
	},
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	out := r.Clone(ctx)
	if r.ContentLength == 0 {
		out.Body = nil
	}
	out.Close = false
	out.RequestURI = ""
	h.rewriteURL(out)

	wantsTrailers := teHasTrailers(r.Header)
	removeHopHeaders(out.Header)
	if wantsTrailers {
		out.Header.Set("Te", "trailers")
	}
	setForwardedHeaders(out, r)

	res, err := h.transport.RoundTrip(out)
	if err != nil {
		h.handleError(w, r, err)
		return
	}
	defer res.Body.Close()

	removeHopHeaders(res.Header)
	copyHeader(w.Header(), res.Header)

	if (len(res.Trailer)) > 0 {
		names := make([]string, 0, len(res.Trailer))
		for k := range res.Trailer {
			names = append(names, k)
		}
		w.Header().Set("Trailer", strings.Join(names, ","))
	}
	w.WriteHeader(res.StatusCode)

	if err := copyBody(w, res.Body); err != nil {
		h.logger.Warn("response body copy failed", "path", r.URL.Path, "err", err)
		panic(http.ErrAbortHandler)
	}

	for k, vv := range res.Trailer {
		w.Header()[k] = vv
	}

}
