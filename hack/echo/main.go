package main

import (
	"encoding/json"
	"flag"
	"io"
	"log"
	"net/http"
	"strconv"
	"time"
)

func main() {
	addr := flag.String("listen", ":9000", "listen address")
	name := flag.String("name", "echo", "instance name, to tell replicas apart")
	flag.Parse()

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if d, err := time.ParseDuration(r.URL.Query().Get("delay")); err == nil {
			select {
			case <-time.After(d):
			case <-r.Context().Done():
				return
			}
		}
		status := http.StatusOK
		if s, err := strconv.Atoi(r.URL.Query().Get("status")); err == nil {
			status = s
		}
		body, _ := io.ReadAll(r.Body)

		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Echo-Server", *name)
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"server": *name,
			"method": r.Method,
			"host":   r.Host,
			"path":   r.URL.Path,
			"query":  r.URL.RawQuery,
			"header": r.Header,
			"body":   string(body),
		})
	})

	log.Printf("echo %q listening on %s", *name, *addr)
	log.Fatal(http.ListenAndServe(*addr, nil))
}
