package agent

import "strings"

func userEmail(uuid, tag string) string {
	return uuid + "@" + tag
}

func splitEmail(email string) (uuid, tag string) {
	uuid, tag, _ = strings.Cut(email, "@")
	return uuid, tag
}
