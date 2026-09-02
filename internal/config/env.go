package config

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// getenv is the lookup function a Config is loaded from. os.Getenv satisfies
// it; tests pass a map-backed closure instead of mutating the process
// environment.
type getenv func(string) string

// loader accumulates parse errors so Load reports the first problem with the
// variable name that caused it instead of failing on a zero value later.
type loader struct {
	get getenv
	err error
}

func (l *loader) str(key, def string) string {
	if v := l.get(key); v != "" {
		return v
	}
	return def
}

func (l *loader) boolean(key string, def bool) bool {
	v := l.get(key)
	if v == "" {
		return def
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		l.fail("%s: %q is not a boolean", key, v)
		return def
	}
	return b
}

func (l *loader) duration(key string, def time.Duration) time.Duration {
	v := l.get(key)
	if v == "" {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		l.fail("%s: %q is not a duration", key, v)
		return def
	}
	return d
}

// csv splits a comma-separated list, trimming whitespace and dropping empty
// entries so a trailing comma or a Helm-rendered blank is harmless.
func (l *loader) csv(key string) []string {
	v := l.get(key)
	if v == "" {
		return nil
	}
	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func (l *loader) fail(format string, args ...interface{}) {
	if l.err == nil {
		l.err = fmt.Errorf(format, args...)
	}
}
