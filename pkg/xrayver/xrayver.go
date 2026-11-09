package xrayver

import (
	"fmt"
	"regexp"
	"strconv"
)

const Min = "26.10.10"

var pattern = regexp.MustCompile(`^([0-9]+)\.([0-9]+)\.([0-9]+)$`)

func parse(s string) ([3]int, bool) {
	m := pattern.FindStringSubmatch(s)
	if m == nil {
		return [3]int{}, false
	}
	var out [3]int
	for i := range out {
		n, err := strconv.Atoi(m[i+1])
		if err != nil {
			return [3]int{}, false
		}
		out[i] = n
	}
	return out, true
}

func Older(a, b string) bool {
	x, okA := parse(a)
	y, okB := parse(b)
	if !okA || !okB {
		return false
	}
	for i := range x {
		if x[i] != y[i] {
			return x[i] < y[i]
		}
	}
	return false
}

func Check(s string) error {
	if _, ok := parse(s); !ok {
		return fmt.Errorf("xray version %q is not in the form 26.10.21", s)
	}
	if Older(s, Min) {
		return fmt.Errorf("xray %s is older than %s: it lacks the freedom outbound block on local addresses that protects the node API", s, Min)
	}
	return nil
}
