package proxy

import (
	"net/http"
	"strings"
)

/** HopHeaders apply to a single transport level
connection and forwarded by a proxy**/
var hopHeaders = []string{
	"Connection",
	"Proxy-Connection",
	"Keep-Alive",
	"Proxy-Authenticate",
	"Proxy-Authorization",
	"Te",
	"Trailer",
	"Transfer-Encoding",
	"Upgrade",
}

/** removeHopHeaders deletes the standard hop-by-hop
headers plus any extra header names the sender listed
inside the connection header**/
func removeHopHeaders(h http.Header)  {
	for _, field := range h.Values("Connection") {
		for _, name := range strings.Split(field, ",") {
			if name = strings.TrimSpace(name); name != "" {
				h.Del(name)
			}
		}
	}

	for _, name := range hopHeaders {
		h.Del(name);
	}
}

/** teHasTrailers reports whether the client sent "TE:trailers"
gRPC depends on trailers, so this one TE value must survive the hop**/
func teHasTrailers(h http.Header)  bool{
	for _, v := range h.Values("Te") {
		for _, part := range strings.Split(v, ",") {
			if strings.EqualFold(strings.TrimSpace(part), "trailers") {
				return true;
			}
		}
	}
	return false;
}