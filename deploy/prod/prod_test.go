package prod

import (
	"io/fs"
	"testing"
)

func TestFilesCarryEveryRole(t *testing.T) {
	for _, role := range []string{"core", "panel", "bot", "subs", "node", "monitor"} {
		for _, name := range []string{"compose.yml", ".env.example"} {
			if _, err := fs.Stat(Files, role+"/"+name); err != nil {
				t.Fatalf("%s/%s: %v", role, name, err)
			}
		}
	}
	for _, role := range []string{"panel", "bot", "subs"} {
		if _, err := fs.Stat(Files, role+"/Caddyfile"); err != nil {
			t.Fatalf("%s/Caddyfile: %v", role, err)
		}
	}
}
