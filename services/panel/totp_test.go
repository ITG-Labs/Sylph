package panel

import (
	"encoding/base32"
	"testing"
	"time"
)

var rfcSecret = []byte("12345678901234567890")

func TestTOTPMatchesRFC6238(t *testing.T) {
	for _, tc := range []struct {
		unix int64
		code string
	}{
		{59, "94287082"},
		{1111111109, "07081804"},
		{1111111111, "14050471"},
		{1234567890, "89005924"},
		{2000000000, "69279037"},
		{20000000000, "65353130"},
	} {
		if got := totpCode(rfcSecret, tc.unix/30, 8); got != tc.code {
			t.Errorf("t=%d: got %s want %s", tc.unix, got, tc.code)
		}
		if got := totpCode(rfcSecret, tc.unix/30, 6); got != tc.code[2:] {
			t.Errorf("t=%d six digits: got %s want %s", tc.unix, got, tc.code[2:])
		}
	}
}

func TestTOTPVerifyWindowAndReplay(t *testing.T) {
	now := time.Unix(1111111111, 0)
	step := now.Unix() / 30
	for _, d := range []int64{-1, 0, 1} {
		got, ok := verifyTOTP(rfcSecret, totpCode(rfcSecret, step+d, 6), now, 0)
		if !ok || got != step+d {
			t.Errorf("offset %d: ok=%v step=%d", d, ok, got)
		}
	}
	for _, d := range []int64{-2, 2} {
		if _, ok := verifyTOTP(rfcSecret, totpCode(rfcSecret, step+d, 6), now, 0); ok {
			t.Errorf("offset %d accepted", d)
		}
	}
	code := totpCode(rfcSecret, step, 6)
	if _, ok := verifyTOTP(rfcSecret, code, now, step); ok {
		t.Fatal("same step accepted twice")
	}
	if _, ok := verifyTOTP(rfcSecret, totpCode(rfcSecret, step-1, 6), now, step); ok {
		t.Fatal("older step accepted after newer")
	}
	if _, ok := verifyTOTP(rfcSecret, totpCode(rfcSecret, step+1, 6), now, step); !ok {
		t.Fatal("newer step rejected")
	}
}

func TestTOTPVerifyRejectsMalformed(t *testing.T) {
	now := time.Unix(1111111111, 0)
	good := totpCode(rfcSecret, now.Unix()/30, 6)
	for _, bad := range []string{"", "12345", "1234567", "abcdef", " " + good[1:], good + " ", "０５０４７１"} {
		if _, ok := verifyTOTP(rfcSecret, bad, now, 0); ok {
			t.Errorf("accepted %q", bad)
		}
	}
}

func TestTOTPSecretEncoding(t *testing.T) {
	secret := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(rfcSecret)
	got, err := decodeTOTPSecret(secret)
	if err != nil || string(got) != string(rfcSecret) {
		t.Fatalf("decode: %v %q", err, got)
	}
	if _, err := decodeTOTPSecret("GEZDGNBV"); err == nil {
		t.Fatal("short secret accepted")
	}
	if _, err := decodeTOTPSecret("not base32!"); err == nil {
		t.Fatal("garbage accepted")
	}
}
