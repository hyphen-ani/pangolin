package reload

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hyphen-ani/pangolin/internal/config"
)

const good = `
apiVersion: pangolinhq.dev/v1alpha1
kind: Config
services:
  - name: svc
    upstream: { targets: [{ host: 127.0.0.1, port: 9000 }] }
    routes: [{ name: all, paths: [/] }]
`

func TestLoadSkipsUnchangedAndRejectsBad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pangolin.yaml")
	write := func(s string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	applied := 0
	r := New(path, func(*config.Config, string) error { applied++; return nil },
		slog.New(slog.NewTextHandler(io.Discard, nil)))

	write(good)
	if changed, err := r.Load(); err != nil || !changed {
		t.Fatalf("first load: changed=%v err=%v", changed, err)
	}
	if changed, _ := r.Load(); changed {
		t.Error("unchanged file was reported as changed")
	}

	write("apiVersion: nope\n")
	if _, err := r.Load(); err == nil {
		t.Error("invalid config was accepted")
	}

	write(strings.Replace(good, "9000", "9001", 1))
	if changed, err := r.Load(); err != nil || !changed {
		t.Fatalf("reload after fix: changed=%v err=%v", changed, err)
	}

	if applied != 2 {
		t.Errorf("apply called %d times, want 2", applied)
	}
}
