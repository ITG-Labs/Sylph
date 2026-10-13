package panel

import (
	"crypto/hmac"
	"crypto/sha1"
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	totpPeriod = 30
	totpDigits = 6
)

var totpEncoding = base32.StdEncoding.WithPadding(base32.NoPadding)

func decodeTOTPSecret(s string) ([]byte, error) {
	b, err := totpEncoding.DecodeString(strings.TrimRight(strings.ToUpper(s), "="))
	if err != nil {
		return nil, errors.New("totp secret must be base32")
	}
	if len(b) < 16 {
		return nil, errors.New("totp secret must be at least 128 bits")
	}
	return b, nil
}

func totpCode(secret []byte, step int64, digits int) string {
	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], uint64(step))
	mac := hmac.New(sha1.New, secret)
	mac.Write(msg[:])
	sum := mac.Sum(nil)
	off := sum[len(sum)-1] & 0x0f
	v := binary.BigEndian.Uint32(sum[off:off+4]) & 0x7fffffff
	mod := uint32(1)
	for range digits {
		mod *= 10
	}
	return fmt.Sprintf("%0*d", digits, v%mod)
}

func verifyTOTP(secret []byte, code string, now time.Time, lastStep int64) (int64, bool) {
	if len(code) != totpDigits {
		return 0, false
	}
	for _, c := range code {
		if c < '0' || c > '9' {
			return 0, false
		}
	}
	cur := now.Unix() / totpPeriod
	for _, step := range []int64{cur - 1, cur, cur + 1} {
		if step <= lastStep {
			continue
		}
		if subtle.ConstantTimeCompare([]byte(totpCode(secret, step, totpDigits)), []byte(code)) == 1 {
			return step, true
		}
	}
	return 0, false
}
