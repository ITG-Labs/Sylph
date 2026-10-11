package mtls

import "crypto/x509"

const (
	RoleBot   = "bot"
	RoleSubs  = "subs"
	RoleCron  = "cron"
	RolePanel = "panel"
	RoleNode  = "node"
)

var knownRoles = map[string]bool{
	RoleBot:   true,
	RoleSubs:  true,
	RoleCron:  true,
	RolePanel: true,
	RoleNode:  true,
}

func IsKnownRole(role string) bool {
	return knownRoles[role]
}

func RoleOf(cert *x509.Certificate) (string, bool) {
	if cert == nil || len(cert.Subject.OrganizationalUnit) != 1 {
		return "", false
	}
	role := cert.Subject.OrganizationalUnit[0]
	if !knownRoles[role] {
		return "", false
	}
	return role, true
}
