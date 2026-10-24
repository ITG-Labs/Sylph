package service

import (
	"fmt"
	"html"
	"net/url"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

var botTags = map[string]bool{
	"b": true, "strong": true, "i": true, "em": true, "u": true, "ins": true, "s": true, "strike": true, "del": true,
	"code": true, "pre": true, "blockquote": true, "tg-spoiler": true, "span": true, "a": true,
}

func validBotHTML(field, v string, maxRunes int) error {
	if !utf8.ValidString(v) {
		return fieldErr(field, "must be valid UTF-8")
	}
	if utf8.RuneCountInString(v) > maxRunes {
		return fieldErr(field, fmt.Sprintf("must be at most %d characters", maxRunes))
	}
	for _, r := range v {
		if unicode.IsControl(r) && r != '\n' && r != '\t' {
			return fieldErr(field, "must not contain control characters")
		}
	}
	var stack []string
	for i := 0; i < len(v); {
		switch v[i] {
		case '&':
			end := strings.IndexByte(v[i:], ';')
			if end < 0 || !validEntity(v[i+1:i+end]) {
				return fieldErr(field, "a bare & must be written as &amp;")
			}
			i += end + 1
		case '<':
			end := strings.IndexByte(v[i:], '>')
			if end < 0 {
				return fieldErr(field, "a bare < must be written as &lt;")
			}
			raw := v[i+1 : i+end]
			i += end + 1
			if name, ok := strings.CutPrefix(raw, "/"); ok {
				if len(stack) == 0 || stack[len(stack)-1] != name {
					return fieldErr(field, "closing tag </"+name+"> does not match an open tag")
				}
				stack = stack[:len(stack)-1]
				continue
			}
			name, attrs, _ := strings.Cut(raw, " ")
			if err := openTag(name, attrs, stack); err != "" {
				return fieldErr(field, err)
			}
			stack = append(stack, name)
		default:
			i++
		}
	}
	if len(stack) > 0 {
		return fieldErr(field, "tag <"+stack[len(stack)-1]+"> is not closed")
	}
	return nil
}

func validEntity(e string) bool {
	switch e {
	case "lt", "gt", "amp", "quot":
		return true
	}
	if hex, ok := strings.CutPrefix(e, "#x"); ok {
		_, err := strconv.ParseUint(hex, 16, 32)
		return hex != "" && err == nil
	}
	if dec, ok := strings.CutPrefix(e, "#"); ok {
		_, err := strconv.ParseUint(dec, 10, 32)
		return dec != "" && err == nil
	}
	return false
}

func openTag(name, rawAttrs string, stack []string) string {
	if !botTags[name] {
		return "tag <" + name + "> is not supported by Telegram (allowed: b, i, u, s, a, code, pre, blockquote, tg-spoiler)"
	}
	attrs, ok := parseAttrs(rawAttrs)
	if !ok {
		return "attributes of <" + name + "> must look like key=\"value\""
	}
	for _, open := range stack {
		if open == "code" || (open == "pre" && (name != "code" || stack[len(stack)-1] != "pre")) {
			return "no tags inside <code> or <pre>"
		}
		if open == name && (name == "a" || name == "blockquote") {
			return "<" + name + "> cannot be nested"
		}
	}
	allowed := map[string]bool{}
	switch name {
	case "a":
		href, ok := attrs["href"]
		if !ok {
			return "<a> needs href"
		}
		u, err := url.Parse(html.UnescapeString(href))
		if err != nil || (u.Scheme != "http" && u.Scheme != "https" && u.Scheme != "tg") || (u.Scheme != "tg" && u.Host == "") {
			return "<a href> must be an http(s) or tg:// link"
		}
		allowed["href"] = true
	case "span":
		if attrs["class"] != "tg-spoiler" {
			return `<span> is only allowed as <span class="tg-spoiler">`
		}
		allowed["class"] = true
	case "blockquote":
		allowed["expandable"] = true
	case "code":
		if lang, ok := attrs["class"]; ok {
			if len(stack) == 0 || stack[len(stack)-1] != "pre" || !strings.HasPrefix(lang, "language-") {
				return `<code class="language-…"> is only allowed inside <pre>`
			}
			allowed["class"] = true
		}
	}
	for k := range attrs {
		if !allowed[k] {
			return "attribute " + k + " is not allowed on <" + name + ">"
		}
	}
	return ""
}

func parseAttrs(s string) (map[string]string, bool) {
	attrs := map[string]string{}
	s = strings.TrimSpace(s)
	for s != "" {
		i := strings.IndexAny(s, "= ")
		key := s
		if i >= 0 {
			key = s[:i]
		}
		if _, dup := attrs[key]; key == "" || dup {
			return nil, false
		}
		if i < 0 || s[i] == ' ' {
			attrs[key] = ""
			if i < 0 {
				break
			}
			s = strings.TrimSpace(s[i:])
			continue
		}
		rest := s[i+1:]
		if rest == "" || (rest[0] != '"' && rest[0] != '\'') {
			return nil, false
		}
		end := strings.IndexByte(rest[1:], rest[0])
		if end < 0 {
			return nil, false
		}
		attrs[key] = rest[1 : end+1]
		after := rest[end+2:]
		if after != "" && after[0] != ' ' {
			return nil, false
		}
		s = strings.TrimSpace(after)
	}
	return attrs, true
}
