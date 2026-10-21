package config

import (
	"errors"
	"strings"
	"testing"
)

const validYAML = `
apiVersion: pangolinhq.dev/v1alpha1
kind: Config
services:
  - name: orders-api
    path: /api
    upstream:
      targets:
        - { host: 127.0.0.1, port: 9000 }
        - { host: 127.0.0.1, port: 9001, weight: 50 }
    routes:
      - name: orders
        hosts: [api.example.com, "*.example.com"]
        paths: [/v1/orders, "=/healthz", "~^/v1/orders/[0-9]+$"]
        methods: [get, post]
        headers: { x-version: [v2] }
`

func TestParseValidAppliesDefaults(t *testing.T) {
	cfg, err := Parse(strings.NewReader(validYAML))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	s := cfg.Services[0]
	if s.Upstream.Scheme != "http" || s.Upstream.Algorithm != AlgoRoundRobin {
		t.Errorf("upstream defaults not applied: %+v", s.Upstream)
	}
	if *s.Upstream.Targets[0].Weight != DefaultWeight || *s.Upstream.Targets[1].Weight != 50 {
		t.Error("weights wrong: want default 100 and explicit 50")
	}

	r := s.Routes[0]
	if !*r.StripPath {
		t.Error("strip_path should default to true")
	}
	if r.Methods[0] != "GET" || r.Methods[1] != "POST" {
		t.Errorf("methods not upper-cased: %v", r.Methods)
	}
	if _, ok := r.Headers["X-Version"]; !ok {
		t.Errorf("header key not canonicalised: %v", r.Headers)
	}
}

// cfgWith wraps one or more service entries in a valid config header.
func cfgWith(services string) string {
	return "apiVersion: pangolinhq.dev/v1alpha1\nkind: Config\nservices:\n" + services
}

const (
	okUpstream = "    upstream:\n      targets: [{ host: 127.0.0.1, port: 9000 }]\n"
	okRoutes   = "    routes:\n      - { name: r1, paths: [/] }\n"
)

func TestParseRejectsInvalid(t *testing.T) {
	cases := []struct {
		name, yaml, wantErr string
	}{
		{"wrong apiVersion",
			"apiVersion: v1\nkind: Config\nservices: []\n",
			"apiVersion: must be"},
		{"unknown field",
			cfgWith("  - name: s1\n    colour: blue\n" + okUpstream + okRoutes),
			"field colour not found"},
		{"duplicate service name",
			cfgWith("  - name: s1\n" + okUpstream + okRoutes +
				"  - name: s1\n" + okUpstream + "    routes:\n      - { name: r2, paths: [/x] }\n"),
			"already used"},
		{"no targets",
			cfgWith("  - name: s1\n    upstream: { targets: [] }\n" + okRoutes),
			"at least one target is required"},
		{"bad port",
			cfgWith("  - name: s1\n    upstream:\n      targets: [{ host: a, port: 0 }]\n" + okRoutes),
			"port: must be 1-65535"},
		{"scheme in host",
			cfgWith("  - name: s1\n    upstream:\n      targets: [{ host: \"http://a\", port: 80 }]\n" + okRoutes),
			"must not include a scheme"},
		{"invalid regex",
			cfgWith("  - name: s1\n" + okUpstream + "    routes:\n      - { name: r1, paths: [\"~^/(\"] }\n"),
			"invalid regex"},
		{"unanchored regex",
			cfgWith("  - name: s1\n" + okUpstream + "    routes:\n      - { name: r1, paths: [\"~/admin\"] }\n"),
			"must start with ^/"},
		{"route without hosts or paths",
			cfgWith("  - name: s1\n" + okUpstream + "    routes:\n      - { name: r1, methods: [GET] }\n"),
			"needs at least one of hosts or paths"},
		{"bad method",
			cfgWith("  - name: s1\n" + okUpstream + "    routes:\n      - { name: r1, paths: [/], methods: [FETCH] }\n"),
			"unsupported method"},
		{"mid-host wildcard",
			cfgWith("  - name: s1\n" + okUpstream + "    routes:\n      - { name: r1, hosts: [\"api.*.com\"] }\n"),
			"wildcard"},
		{"multiple documents",
			cfgWith("  - name: s1\n"+okUpstream+okRoutes) + "---\nfoo: bar\n",
			"exactly one YAML document"},
		{"empty file", "", "config is empty"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := Parse(strings.NewReader(c.yaml))
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", c.wantErr)
			}
			if !strings.Contains(err.Error(), c.wantErr) {
				t.Errorf("error = %v\nwant it to contain %q", err, c.wantErr)
			}
		})
	}
}

func TestValidationCollectsAllErrors(t *testing.T) {
	y := cfgWith("  - name: s1\n    upstream:\n      targets: [{ host: a, port: 0 }]\n" +
		"    routes:\n      - { name: r1, paths: [/], methods: [FETCH] }\n")

	_, err := Parse(strings.NewReader(y))

	var ve *ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("expected *ValidationError, got %T: %v", err, err)
	}
	if len(ve.Errors) != 2 {
		t.Errorf("got %d errors, want 2 (port + method):\n%v", len(ve.Errors), err)
	}
}
