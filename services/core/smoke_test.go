package core_test

import (
	"os"
	"testing"
)

func TestEnvContract(t *testing.T) {
	if os.Getenv("SYLPH_TEST_DB") == "" {
		t.Skip("SYLPH_TEST_DB not set, docker-based tests skipped")
	}
}
