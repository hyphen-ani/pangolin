package config

import "strings"

type PathKind int

const (
	PathPrefix PathKind = iota
	PathExact
	PathRegex
)

func (k PathKind) String() string {
	switch k {
	case PathExact:
		return "exact"
	case PathRegex:
		return "regex"
	default:
		return "prefix"
	}
}

func ParsePath(p string) (PathKind, string) {
	switch {
	case strings.HasPrefix(p, "="):
		return PathExact, p[1:]
	case strings.HasPrefix(p, "~"):
		return PathRegex, p[1:]
	default:
		return PathPrefix, p
	}
}
