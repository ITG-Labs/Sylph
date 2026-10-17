package ui

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
)

func TestQRIsSquareAndScannableSize(t *testing.T) {
	out, err := QR("otpauth://totp/Sylph:owner?secret=JBSWY3DPEHPK3PXP&issuer=Sylph&algorithm=SHA1&digits=6&period=30")
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(out, "\n")
	if len(lines) < 10 {
		t.Fatalf("qr has %d lines", len(lines))
	}
}

func TestMask(t *testing.T) {
	if got := Mask("live_abcdef1234"); got != "••••1234" || strings.Contains(got, "abcdef") {
		t.Fatalf("Mask = %q", got)
	}
	if got := Mask("ab"); strings.Contains(got, "ab") && got != "••••ab" {
		t.Fatalf("short Mask = %q", got)
	}
}

func TestPlainStep(t *testing.T) {
	var buf bytes.Buffer
	u := &UI{Out: &buf}
	detail, err := u.Step(context.Background(), "Образы", func(ctx context.Context, set func(string)) (string, error) {
		set("тяну")
		return "3 образа", nil
	})
	if err != nil || detail != "3 образа" {
		t.Fatalf("step = %q, %v", detail, err)
	}
	if _, err := u.Step(context.Background(), "Часы", func(ctx context.Context, set func(string)) (string, error) {
		return "", WarnError{"не синхронизированы"}
	}); !IsWarn(err) {
		t.Fatalf("warn err = %v", err)
	}
	if _, err := u.Step(context.Background(), "Docker", func(ctx context.Context, set func(string)) (string, error) {
		return "", errors.New("не отвечает")
	}); err == nil || IsWarn(err) {
		t.Fatalf("fail err = %v", err)
	}
	out := buf.String()
	for _, want := range []string{"✓ Образы", "3 образа", "! Часы", "✗ Docker", "не отвечает"} {
		if !strings.Contains(out, want) {
			t.Fatalf("output lacks %q:\n%s", want, out)
		}
	}
}

func TestKV(t *testing.T) {
	out := KV([][2]string{{"роль", "core"}, {"версия образов", "0.1.0"}})
	if !strings.Contains(out, "core") || !strings.Contains(out, "0.1.0") || strings.Count(out, "\n") != 1 {
		t.Fatalf("KV = %q", out)
	}
}
