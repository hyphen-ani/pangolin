package router

import (
	"errors"
	"fmt"
	"maps"
	"net"
	"net/http"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/hyphen-ani/pangolin/internal/config"
)

type Route struct {
	Name         string
	Service      string
	StripPath    bool
	PreserveHost bool

	methods  map[string]bool
	headers  []headerMatch
	priority int
	order    int
}

type headerMatch struct {
	name  string
	value []string
}

type Match struct {
	Route         *Route
	MatchedPrefix string
}

type Router struct {
	exactHosts    map[string]*pathTable
	wildcardHosts []wildcardHost
	anyHost       *pathTable
}

type wildcardHost struct {
	suffix string
	table  *pathTable
}

type pathTable struct {
	exact  map[string][]*Route
	regex  []regexEntry
	prefix *trieNode
}

type regexEntry struct {
	re    *regexp.Regexp
	route *Route
}

func newPathTable() *pathTable {
	return &pathTable{
		exact:  make(map[string][]*Route),
		prefix: &trieNode{}}
}

type requestView struct {
	method string
	header http.Header
}

/** BUILD **/

type builder struct {
	rt   *Router
	seen map[string]string
}

func New(cfg *config.Config) (*Router, error) {
	b := &builder{
		rt:   &Router{exactHosts: map[string]*pathTable{}},
		seen: map[string]string{},
	}

	var errs []error
	order := 0

	for _, svc := range cfg.Services {
		for _, rc := range svc.Routes {
			r := compileRoute(svc.Name, rc, order)
			order++

			hosts := rc.Hosts
			if len(hosts) == 0 {
				hosts = []string{""}
			}

			paths := rc.Paths
			if len(paths) == 0 {
				paths = []string{"/"}
			}

			for _, h := range hosts {
				t := b.table(h)
				for _, p := range paths {
					if err := b.add(t, h, p, r); err != nil {
						errs = append(errs, err)
					}
				}
			}
		}
	}
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}

	b.finalize()
	return b.rt, nil
}

func compileRoute(service string, rc config.Route, order int) *Route {
	r := &Route{
		Name:         rc.Name,
		Service:      service,
		StripPath:    rc.StripPath == nil || *rc.StripPath,
		PreserveHost: rc.PreserveHost,
		priority:     rc.Priority,
		order:        order,
	}

	if len(rc.Methods) > 0 {
		r.methods = make(map[string]bool, len(rc.Methods))
		for _, m := range rc.Methods {
			r.methods[m] = true
		}
	}

	for _, k := range slices.Sorted(maps.Keys(rc.Headers)) {
		r.headers = append(r.headers, headerMatch{
			name:  k,
			value: rc.Headers[k],
		})
	}

	return r
}

func (b *builder) table(host string) *pathTable {
	switch {
	case host == "":
		if b.rt.anyHost == nil {
			b.rt.anyHost = newPathTable()
		}
		return b.rt.anyHost
	case strings.HasPrefix(host, "*."):
		suffix := host[1:]
		for _, w := range b.rt.wildcardHosts {
			if w.suffix == suffix {
				return w.table
			}
		}
		t := newPathTable()
		b.rt.wildcardHosts = append(b.rt.wildcardHosts, wildcardHost{suffix: suffix, table: t})
		return t
	default:
		t, ok := b.rt.exactHosts[host]
		if !ok {
			t = newPathTable()
			b.rt.exactHosts[host] = t
		}
		return t
	}
}

func (b *builder) add(t *pathTable, host, path string, r *Route) error {
	key := conflictKey(host, path, r)
	if prev, dup := b.seen[key]; dup {
		return fmt.Errorf("routes %q and %q are identical (host %q, path %q, same methods, headers and priority); one could never match",
			prev, r.Name, host, path)
	}
	b.seen[key] = r.Name

	kind, val := config.ParsePath(path)
	switch kind {
	case config.PathExact:
		t.exact[val] = insertSorted(t.exact[host], r)
	case config.PathRegex:
		re, err := regexp.Compile(val)
		if err != nil {
			return fmt.Errorf("route %q: %w", r.Name, err)
		}
		t.regex = append(t.regex, regexEntry{re: re, route: r})
	default:
		t.prefix.insert(val, r)
	}
	return nil
}

func (b *builder) finalize() {
	sort.SliceStable(b.rt.wildcardHosts, func(i, j int) bool {
		return len(b.rt.wildcardHosts[i].suffix) > len(b.rt.wildcardHosts[j].suffix)
	})

	tables := slices.Collect(maps.Values(b.rt.exactHosts))
	for _, w := range b.rt.wildcardHosts {
		tables = append(tables, w.table)
	}

	if b.rt.anyHost != nil {
		tables = append(tables, b.rt.anyHost)
	}

	for _, t := range tables {
		sort.SliceStable(t.regex, func(i, j int) bool {
			return less(t.regex[i].route, t.regex[j].route)
		})
	}
}

func conflictKey(host, path string, r *Route) string {
	methods := slices.Sorted(maps.Keys(r.methods))
	var hdr strings.Builder
	for _, h := range r.headers {
		vals := slices.Clone(h.value)
		slices.Sort(vals)
		fmt.Fprintf(&hdr, "%s=%s;", h.name, strings.Join(vals, ","))
	}
	return fmt.Sprintf("%s|%s|%s|%s|%d", host, path, strings.Join(methods, ","), hdr.String(), r.priority)
}

/** LOOKUP **/

func (rt *Router) Match(req *http.Request) (Match, bool) {
	host := normalizeHost(req.Host)
	path := req.URL.Path
	if path == "" {
		path = "/"
	}

	view := &requestView{method: req.Method, header: req.Header}

	if t, ok := rt.exactHosts[host]; ok {
		if m, ok := t.match(path, view); ok {
			return m, true
		}
	}

	for _, w := range rt.wildcardHosts {
		if strings.HasSuffix(host, w.suffix) {
			if m, ok := w.table.match(path, view); ok {
				return m, true
			}
		}
	}

	if rt.anyHost != nil {
		return rt.anyHost.match(path, view)
	}

	return Match{}, false

}

func (t *pathTable) match(path string, req *requestView) (Match, bool) {
	if rs, ok := t.exact[path]; ok {
		if r := firstMatching(rs, req); r != nil {
			return Match{Route: r, MatchedPrefix: path}, true
		}
	}

	for _, e := range t.regex {
		if e.re.MatchString(path) && e.route.matches(req) {
			return Match{Route: e.route}, true
		}
	}

	if r, prefix := t.prefix.longest(path, req); r != nil {
		return Match{Route: r, MatchedPrefix: prefix}, true
	}

	return Match{}, false
}

func (r *Route) matches(req *requestView) bool {
	if r.methods != nil && !r.methods[req.method] {
		return false
	}

	for _, h := range r.headers {
		if !containAny(req.header[h.name], h.value) {
			return false
		}
	}
	return true
}

func firstMatching(rs []*Route, req *requestView) *Route {
	for _, r := range rs {
		if r.matches(req) {
			return r
		}
	}
	return nil
}

func containAny(got, want []string) bool {
	for _, g := range got {
		for _, w := range want {
			if g == w {
				return true
			}
		}
	}
	return false
}

/** HELPERS - ORDERING **/

func less(a, b *Route) bool {
	if a.priority != b.priority {
		return a.priority > b.priority
	}

	if sa, sb := a.specificity(), b.specificity(); sa != sb {
		return sa > sb
	}

	return a.order < b.order
}

func (r *Route) specificity() int {
	s := 2 * len(r.headers)
	if r.methods != nil {
		s++
	}
	return s
}

func insertSorted(list []*Route, r *Route) []*Route {
	i := sort.Search(len(list), func(i int) bool {
		return less(r, list[i])
	})

	list = append(list, nil)
	copy(list[i+1:], list[i:])
	list[i] = r
	return list
}

func normalizeHost(h string) string {
	if host, _, err := net.SplitHostPort(h); err == nil {
		h = host
	}
	return strings.TrimSuffix(strings.ToLower(h), ".")
}
