package envfile

import (
	"bufio"
	"bytes"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

var keyPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

var plainValue = regexp.MustCompile(`^[A-Za-z0-9_./:@,+=-]*$`)

type Env struct {
	keys   []string
	values map[string]string
}

func New() *Env {
	return &Env{values: map[string]string{}}
}

func Parse(data []byte) (*Env, error) {
	env := New()
	sc := bufio.NewScanner(bytes.NewReader(data))
	n := 0
	for sc.Scan() {
		n++
		key, value, ok, err := parseLine(sc.Text())
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", n, err)
		}
		if ok {
			env.Set(key, value)
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return env, nil
}

func parseLine(line string) (string, string, bool, error) {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" || strings.HasPrefix(trimmed, "#") {
		return "", "", false, nil
	}
	trimmed = strings.TrimPrefix(trimmed, "export ")
	key, raw, found := strings.Cut(trimmed, "=")
	key = strings.TrimSpace(key)
	if !found || !keyPattern.MatchString(key) {
		return "", "", false, fmt.Errorf("expected KEY=VALUE")
	}
	raw = strings.TrimSpace(raw)
	switch {
	case strings.HasPrefix(raw, "'"):
		end := strings.Index(raw[1:], "'")
		if end < 0 {
			return "", "", false, fmt.Errorf("unterminated quote in %s", key)
		}
		return key, raw[1 : end+1], true, nil
	case strings.HasPrefix(raw, `"`):
		end := strings.Index(raw[1:], `"`)
		if end < 0 {
			return "", "", false, fmt.Errorf("unterminated quote in %s", key)
		}
		return key, raw[1 : end+1], true, nil
	}
	if i := strings.Index(raw, " #"); i >= 0 {
		raw = strings.TrimSpace(raw[:i])
	}
	return key, raw, true, nil
}

func (e *Env) Get(key string) string {
	return e.values[key]
}

func (e *Env) Lookup(key string) (string, bool) {
	v, ok := e.values[key]
	return v, ok
}

func (e *Env) Set(key string, value string) {
	if _, ok := e.values[key]; !ok {
		e.keys = append(e.keys, key)
	}
	e.values[key] = value
}

func (e *Env) Keys() []string {
	return append([]string(nil), e.keys...)
}

func (e *Env) Clone() *Env {
	c := New()
	for _, k := range e.keys {
		c.Set(k, e.values[k])
	}
	return c
}

func (e *Env) Merge(other *Env) {
	for _, k := range other.keys {
		e.Set(k, other.values[k])
	}
}

func Quote(value string) string {
	if plainValue.MatchString(value) {
		return value
	}
	return "'" + value + "'"
}

func ValidValue(value string) error {
	if strings.ContainsAny(value, "'\n\r") {
		return fmt.Errorf("value must not contain quotes or line breaks")
	}
	return nil
}

func Render(template []byte, values *Env) ([]byte, error) {
	var out bytes.Buffer
	used := map[string]bool{}
	sc := bufio.NewScanner(bytes.NewReader(template))
	for sc.Scan() {
		line := sc.Text()
		key, _, ok, err := parseLine(line)
		if err != nil {
			return nil, err
		}
		if ok {
			if v, has := values.Lookup(key); has {
				if err := ValidValue(v); err != nil {
					return nil, fmt.Errorf("%s: %w", key, err)
				}
				line = key + "=" + Quote(v)
			}
			used[key] = true
		}
		out.WriteString(line)
		out.WriteByte('\n')
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	var extra []string
	for _, k := range values.keys {
		if !used[k] {
			extra = append(extra, k)
		}
	}
	if len(extra) > 0 {
		out.WriteString("\n# Свои переменные (их нет в примере роли)\n")
		for _, k := range extra {
			if err := ValidValue(values.values[k]); err != nil {
				return nil, fmt.Errorf("%s: %w", k, err)
			}
			out.WriteString(k + "=" + Quote(values.values[k]) + "\n")
		}
	}
	return out.Bytes(), nil
}

func SortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
