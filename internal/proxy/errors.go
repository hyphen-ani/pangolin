package proxy

import (
	"encoding/json"
	"net/http"
)

/** writeError sends a small JSON error. Every supposed error
pangolin generates uses this shape. **/
func writeError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(struct {
		Message string `json:"message"`
	}{msg})
}
