package config

import (
	"fmt"
	"maps"
	"net"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

type FieldError struct {
	Field string
	Msg   string
}

func (e FieldError) Error() string {
	return e.Field + ": " + e.Msg
}

type ValidationError struct {
	Errors []FieldError
}

type validator struct {
	errs []FieldError
}

var (
	nameRe      = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)
	hostLabelRe = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)

	validMethods = map[string]bool{
		"GET": true, "POST": true, "HEAD": true, "PUT": true,
		"PATCH": true, "DELETE": true, "OPTIONS": true, "TRACE": true,
	}
)

func (e *ValidationError) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "config has %d error(s):", len(e.Errors))
	for _, fe := range e.Errors {
		b.WriteString("\n  - ")
		b.WriteString(fe.Error())
	}

	return b.String()
}

func Validate(c *Config) error {
	v := &validator{}

	if c.APIVersion != APIVersion {
		v.add("apiVersion", "must be %q, got %q", APIVersion, c.APIVersion)
	}

	if c.Kind != Kind {
		v.add("kind", "must be %q, got %q", Kind, c.Kind)
	}

	serviceNames := map[string]string{}
	routeNames := map[string]string{}

	for i := range c.Services {
		v.service(fmt.Sprintf("services[%d]", i), &c.Services[i], serviceNames, routeNames)
	}

	if len(v.errs) > 0 {
		return &ValidationError{Errors: v.errs}
	}

	return nil
}

func (v *validator) service(f string, s *Service, serviceNames, routeNames map[string]string) {
	v.name(f+".name", s.Name, serviceNames)

	if s.Path != "" && !strings.HasPrefix(s.Path, "/") {
		v.add(f+".path", "%q must start with '/'", s.Path)
	}

	v.upstream(f+".upstream", &s.Upstream)

	if len(s.Routes) == 0 {
		v.add(f+".routes", "at least one route is required")
	}
	for j := range s.Routes {
		v.route(fmt.Sprintf("%s.routes[%d]", f, j), &s.Routes[j], routeNames)
	}
}

func (v *validator) add(field, format string, args ...any) {
	v.errs = append(v.errs, FieldError{
		Field: field,
		Msg:   fmt.Sprintf(format, args...),
	})
}

func (v *validator) name(field, name string, seen map[string]string) {
	switch {
	case name == "":
		v.add(field, "is required")
	case !nameRe.MatchString(name):
		v.add(field, "%q must be lowercase letters, digits and '-' (max 63 chars)", name)
	default:
		if prev, dup := seen[name]; dup {
			v.add(field, "%q is already used by %s", name, prev)
			return
		}

		seen[name] = field
	}
}

func (v *validator) route(f string, r *Route, routeNames map[string]string) {
	v.name(f+".name", r.Name, routeNames)

	if len(r.Hosts) == 0 && len(r.Paths) == 0 {
		v.add(f, "needs at least one of hosts or paths")
	}

	for i, h := range r.Hosts {
		v.host(fmt.Sprintf("%s.hosts[%d]", f, i), h)
	}

	for i, p := range r.Paths {
		v.path(fmt.Sprintf("%s.paths[%d]", f, i), p)
	}

	for i, m := range r.Methods {
		if !validMethods[m] {
			v.add(fmt.Sprintf("%s.methods[%d]", f, i), "unsupported method %q", m)
		}
	}

	for _, k := range slices.Sorted(maps.Keys(r.Headers)) {
		hf := fmt.Sprintf("%s.headers[%s]", f, k)
		if k == "" || strings.ContainsAny(k, " :\t") {
			v.add(hf, "invalid header name %q", k)
		}
		if len(r.Headers[k]) == 0 {
			v.add(hf, "needs at least one value")
		}
	}

	if r.Priority < 0 || r.Priority > 1000 {
		v.add(f+".priority", "must be 0-1000, got %d", r.Priority)
	}
}

func (v *validator) host(f, h string) {
	name := strings.TrimPrefix(h, "*.")
	switch {
	case strings.Contains(name, "*"):
		v.add(f, "%q: only a single leading '*.' wildcard is allowed", h)
		return
	case strings.Contains(name, ":"):
		v.add(f, "%q: ports are not allowed in route hosts", h)
		return
	case len(name) == 0 || len(name) > 253:
		v.add(f, "%q is not a valid hostname", h)
		return
	}

	for _, label := range strings.Split(name, ".") {
		if !hostLabelRe.MatchString(label) {
			v.add(f, "%q is not a valid hostname", h)
			return
		}
	}
}

func (v *validator) path(f, p string) {
	kind, val := ParsePath(p)
	if kind == PathRegex {
		if _, err := regexp.Compile(val); err != nil {
			v.add(f, "invalid regexp %q: %v", val, err)
			return
		}

		if !strings.HasPrefix(val, "^/") {
			v.add(f, "regex %q must start with ^/ so it matches from the start of the path", val)
		}
		return
	}

	if !strings.HasPrefix(val, "/") {
		v.add(f, "%q must start with '/' (use '=/' for exact, '~^/' for regex)", p)
	}

	if strings.ContainsAny(val, "?#") {
		v.add(f, "%q must not contain a query string or fragment", p)
	}
}

func (v *validator) upstream(f string, u *Upstream) {
	if u.Scheme != "http" && u.Scheme != "https" {
		v.add(f+".scheme", "must be http or https, got %q", u.Scheme)
	}

	if u.Algorithm != AlgoRoundRobin && u.Algorithm != AlgoLeastConnections {
		v.add(f+".algorithm", "must be %s or %s, got %q", AlgoRoundRobin, AlgoLeastConnections, u.Algorithm)
	}

	if len(u.Targets) == 0 {
		v.add(f+".targets", "at least one target is required")
		return
	}

	seen := map[string]string{}
	active := 0

	for i, t := range u.Targets {
		tf := fmt.Sprintf("%s.targets[%d]", f, i)

		switch {
		case t.Host == "":
			v.add(tf+".host", "is required")
		case strings.Contains(t.Host, "://"):
			v.add(tf+".host", "%q must not include a scheme; set upstream.scheme instead", t.Host)
		}
		if t.Port < 1 || t.Port > 65535 {
			v.add(tf+".port", "must be 1-65535, got %d", t.Port)
		}

		if w := *t.Weight; w < 0 || w > 1000 {
			v.add(tf+".weight", "must be 0-1000, got %d", w)
		} else if w > 0 {
			active++
		}

		addr := net.JoinHostPort(t.Host, strconv.Itoa(t.Port))
		if prev, dup := seen[addr]; dup {
			v.add(tf, "%s duplicates %s", addr, prev)
		}
		seen[addr] = tf
	}
	if active == 0 {
		v.add(f+".targets", "at least one target needs weight > 0")
	}
}
