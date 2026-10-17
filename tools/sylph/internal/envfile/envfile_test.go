package envfile

import (
	"strings"
	"testing"
)

const example = `# Версия образов
SYLPH_VERSION=0.1.0

# Токен
SYLPH_BOT_TOKEN=
SYLPH_BOT_TZ=Europe/Moscow
`

func TestParse(t *testing.T) {
	env, err := Parse([]byte("# c\nA=1\nexport B='x y'\nC=\"q\"\nD=v # note\n\nE=\n"))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"A": "1", "B": "x y", "C": "q", "D": "v", "E": ""}
	for k, v := range want {
		if got, ok := env.Lookup(k); !ok || got != v {
			t.Fatalf("%s = %q, %v; want %q", k, got, ok, v)
		}
	}
	if strings.Join(env.Keys(), ",") != "A,B,C,D,E" {
		t.Fatalf("keys = %v", env.Keys())
	}
}

func TestParseRejectsGarbage(t *testing.T) {
	for _, in := range []string{"just text\n", "1A=2\n", "A='open\n"} {
		if _, err := Parse([]byte(in)); err == nil {
			t.Fatalf("Parse(%q) should fail", in)
		}
	}
}

func TestRenderKeepsCommentsOrderAndExtras(t *testing.T) {
	values := New()
	values.Set("MY_OWN", "keep")
	values.Set("SYLPH_BOT_TOKEN", "123:abc")
	values.Set("SYLPH_VERSION", "0.2.0")
	out, err := Render([]byte(example), values)
	if err != nil {
		t.Fatal(err)
	}
	want := `# Версия образов
SYLPH_VERSION=0.2.0

# Токен
SYLPH_BOT_TOKEN=123:abc
SYLPH_BOT_TZ=Europe/Moscow

# Свои переменные (их нет в примере роли)
MY_OWN=keep
`
	if string(out) != want {
		t.Fatalf("got:\n%s\nwant:\n%s", out, want)
	}
	again, err := Parse(out)
	if err != nil {
		t.Fatal(err)
	}
	reRendered, err := Render([]byte(example), again)
	if err != nil {
		t.Fatal(err)
	}
	if string(reRendered) != want {
		t.Fatalf("render is not stable:\n%s", reRendered)
	}
}

func TestRenderQuotesUnsafeValues(t *testing.T) {
	values := New()
	values.Set("SYLPH_BOT_TOKEN", "a b$c#d")
	out, err := Render([]byte(example), values)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "SYLPH_BOT_TOKEN='a b$c#d'\n") {
		t.Fatalf("got:\n%s", out)
	}
	back, err := Parse(out)
	if err != nil {
		t.Fatal(err)
	}
	if back.Get("SYLPH_BOT_TOKEN") != "a b$c#d" {
		t.Fatalf("round trip = %q", back.Get("SYLPH_BOT_TOKEN"))
	}
}

func TestRenderRejectsQuotesAndNewlines(t *testing.T) {
	for _, v := range []string{"it's", "a\nb"} {
		values := New()
		values.Set("SYLPH_BOT_TOKEN", v)
		if _, err := Render([]byte(example), values); err == nil {
			t.Fatalf("Render with %q should fail", v)
		}
	}
}
