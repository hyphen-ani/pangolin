package config

import (
	"net/http"
	"strings"
)

func applyDefaults(c *Config) {
	for i := range c.Services {
		s := &c.Services[i]

		if s.Upstream.Scheme == "" {
			s.Upstream.Scheme = "http"
		}

		if s.Upstream.Algorithm == "" {
			s.Upstream.Algorithm = AlgoRoundRobin
		}

		for j := range s.Upstream.Targets {
			t := &s.Upstream.Targets[j]
			if t.Weight == nil {
				w := DefaultWeight
				t.Weight = &w
			}
		}

		for j := range s.Routes {
			r := &s.Routes[j]
			if r.StripPath == nil {
				v := true
				r.StripPath = &v
			}

			for k, m := range r.Methods {
				r.Methods[k] = strings.ToUpper(m)
			}

			for k, h := range r.Hosts {
				r.Hosts[k] = strings.ToLower(h)
			}

			if len(r.Headers) > 0 {
				canon := make(map[string][]string, len(r.Headers))
				for k, v := range r.Headers {
					ck := http.CanonicalHeaderKey(k)
					canon[ck] = append(canon[ck], v...)
				}

				r.Headers = canon
			}
		}
	}
}
