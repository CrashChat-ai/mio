package auth

import (
	"fmt"
	"strings"
)

type Role string

const (
	RoleViewer          Role = "viewer"
	RoleOperator        Role = "operator"
	RoleCredentialAdmin Role = "credential-admin"
)

type RoleAssignments struct {
	Default Role
	emails  map[string]Role
	domains map[string]Role
}

func ParseRole(raw string) (Role, error) {
	switch Role(strings.ToLower(strings.TrimSpace(raw))) {
	case "", RoleViewer:
		return RoleViewer, nil
	case RoleOperator:
		return RoleOperator, nil
	case RoleCredentialAdmin:
		return RoleCredentialAdmin, nil
	default:
		return "", fmt.Errorf("auth: unsupported operator role %q", raw)
	}
}

func ParseRoleAssignments(spec string, defaultRole Role) (RoleAssignments, error) {
	if defaultRole == "" {
		defaultRole = RoleViewer
	}
	defaultRole, err := ParseRole(string(defaultRole))
	if err != nil {
		return RoleAssignments{}, err
	}
	out := RoleAssignments{
		Default: defaultRole,
		emails:  map[string]Role{},
		domains: map[string]Role{},
	}
	for _, part := range strings.Split(spec, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		key, value, ok := strings.Cut(part, "=")
		if !ok {
			key, value, ok = strings.Cut(part, ":")
		}
		if !ok {
			return RoleAssignments{}, fmt.Errorf("auth: role assignment %q must use email=role or domain=role", part)
		}
		key = strings.ToLower(strings.TrimSpace(key))
		role, err := ParseRole(value)
		if err != nil {
			return RoleAssignments{}, err
		}
		if key == "" {
			return RoleAssignments{}, fmt.Errorf("auth: role assignment %q has empty principal", part)
		}
		if strings.Contains(key, "@") && !strings.HasPrefix(key, "@") {
			out.emails[key] = role
			continue
		}
		key = strings.TrimPrefix(key, "@")
		if key == "" {
			return RoleAssignments{}, fmt.Errorf("auth: role assignment %q has empty domain", part)
		}
		out.domains[key] = role
	}
	return out, nil
}

func (r RoleAssignments) RoleForEmail(email string) Role {
	email = strings.ToLower(strings.TrimSpace(email))
	if role, ok := r.emails[email]; ok {
		return role
	}
	if at := strings.LastIndex(email, "@"); at >= 0 && at < len(email)-1 {
		if role, ok := r.domains[email[at+1:]]; ok {
			return role
		}
	}
	if r.Default == "" {
		return RoleViewer
	}
	return r.Default
}

func RoleAllows(actual, required Role) bool {
	return roleRank(actual) >= roleRank(required)
}

func roleRank(role Role) int {
	switch role {
	case RoleCredentialAdmin:
		return 3
	case RoleOperator:
		return 2
	case RoleViewer, "":
		return 1
	default:
		return 0
	}
}
