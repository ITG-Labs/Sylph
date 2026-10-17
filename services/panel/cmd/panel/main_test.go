package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/ITG-Labs/sylph/services/panel"
)

func TestNewAdminJSON(t *testing.T) {
	var out bytes.Buffer
	if err := newAdmin([]string{"--name", "owner", "--json"}, &out); err != nil {
		t.Fatal(err)
	}
	var got struct {
		Record   panel.AdminRecord `json:"record"`
		Password string            `json:"password"`
		TOTPURI  string            `json:"totp_uri"`
	}
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("output is not one JSON object: %v\n%s", err, out.String())
	}
	if got.Record.Name != "owner" || got.Record.PasswordHash == "" || got.Record.TOTPSecret == "" {
		t.Fatalf("record = %+v", got.Record)
	}
	if got.Password == "" || !strings.HasPrefix(got.TOTPURI, "otpauth://totp/") || !strings.Contains(got.TOTPURI, got.Record.TOTPSecret) {
		t.Fatalf("password = %q, uri = %q", got.Password, got.TOTPURI)
	}
}

func TestNewAdminHumanOutputUnchanged(t *testing.T) {
	var out bytes.Buffer
	if err := newAdmin([]string{"--name", "owner"}, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(out.String(), "Запись для файла админов") {
		t.Fatalf("output = %q", out.String())
	}
}
