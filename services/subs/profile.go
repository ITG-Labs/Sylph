package subs

import "fmt"

const (
	groupSelect = "Выбор сервера"
	groupAuto   = "Автовыбор"
	probeURL    = "https://www.gstatic.com/generate_204"
)

var reservedNames = []string{groupSelect, groupAuto, "direct", "DIRECT", "REJECT", "REJECT-DROP", "PASS", "COMPATIBLE", "GLOBAL"}

func profileNames(conns []Connection) []string {
	taken := make(map[string]bool, len(conns)+len(reservedNames))
	for _, r := range reservedNames {
		taken[r] = true
	}
	names := make([]string, len(conns))
	for i, c := range conns {
		name := c.Name
		for n := 2; taken[name]; n++ {
			name = fmt.Sprintf("%s %d", c.Name, n)
		}
		taken[name] = true
		names[i] = name
	}
	return names
}

func stubConnection(text string, uuid string) Connection {
	return Connection{Name: text, Address: "127.0.0.1", Port: 1, UUID: uuid, Transport: "tcp", Security: "none"}
}
