package xrayver

import "testing"

func TestCheck(t *testing.T) {
	for _, v := range []string{Min, "26.10.21", "26.11.1", "27.1.1", "100.0.0"} {
		if err := Check(v); err != nil {
			t.Errorf("%s: %v", v, err)
		}
	}
	for _, v := range []string{"26.3.27", "26.10.9", "25.12.31", "1.8.24", "0.0.0"} {
		if err := Check(v); err == nil {
			t.Errorf("%s must be refused", v)
		}
	}
	for _, v := range []string{"", "v26.10.21", "26.10", "26.10.x", "latest"} {
		if err := Check(v); err == nil {
			t.Errorf("%q must be refused", v)
		}
	}
}

func TestOlder(t *testing.T) {
	if !Older("26.3.27", "26.10.10") || Older("26.10.10", "26.10.10") || Older("26.10.21", "26.10.10") || !Older("25.12.31", "26.1.1") {
		t.Fatal("Older compares numerically by year, month, day")
	}
}
