package rest

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// normalizePath replaces any {param} placeholder with {id} for shape comparison.
func normalizePath(p string) string {
	return regexp.MustCompile(`\{[^}]+\}`).ReplaceAllString(p, "{id}")
}

// TestAdminRouteParityWithProto asserts set-equality between:
//   - proto-annotated admin RPC paths (from the committed generated spec), and
//   - admin router routes (router.go adminMux registrations, excluding BFF-only routes).
//
// A new RPC with google.api.http but no router handler → FAIL.
// A router admin route with no proto annotation → FAIL (unless in bffOnlyRoutes).
func TestAdminRouteParityWithProto(t *testing.T) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	repoRoot := filepath.Join(filepath.Dir(thisFile), "..", "..", "..", "..")

	specPath := filepath.Join(repoRoot, "ui", "web", "api-contracts", "gen", "mio", "admin", "v1", "admin.openapi.yaml")
	specBytes, err := os.ReadFile(specPath)
	if err != nil {
		t.Fatalf("read proto-derived spec (run `make proto` to regenerate): %v", err)
	}

	var spec struct {
		Paths map[string]map[string]any `yaml:"paths"`
	}
	if err := yaml.Unmarshal(specBytes, &spec); err != nil {
		t.Fatalf("parse proto-derived spec: %v", err)
	}

	protoRoutes := make(map[string]struct{})
	for path, methods := range spec.Paths {
		norm := normalizePath(path)
		for method := range methods {
			if isHTTPMethod(method) {
				protoRoutes[strings.ToUpper(method)+" "+norm] = struct{}{}
			}
		}
	}

	// BFF-only admin routes: in router but NOT in proto by design.
	bffOnly := map[string]struct{}{
		"GET /api/admin/audit": {},
	}

	routerPath := filepath.Join(repoRoot, "ui", "web", "internal", "rest", "router.go")
	routerSrc, err := os.ReadFile(routerPath)
	if err != nil {
		t.Fatalf("read router.go: %v", err)
	}
	routerRoutes := extractAdminRouterRoutes(string(routerSrc))

	var missingInRouter []string
	for r := range protoRoutes {
		if _, ok := routerRoutes[r]; !ok {
			missingInRouter = append(missingInRouter, r)
		}
	}
	sort.Strings(missingInRouter)

	var unaccountedRouter []string
	for r := range routerRoutes {
		_, inProto := protoRoutes[r]
		_, isBFF := bffOnly[r]
		if !inProto && !isBFF {
			unaccountedRouter = append(unaccountedRouter, r)
		}
	}
	sort.Strings(unaccountedRouter)

	if len(missingInRouter) > 0 {
		t.Errorf("proto RPCs with google.api.http but no router handler (invisible endpoint):\n  %s",
			strings.Join(missingInRouter, "\n  "))
	}
	if len(unaccountedRouter) > 0 {
		t.Errorf("router admin routes with no proto annotation and not in bffOnly:\n  %s",
			strings.Join(unaccountedRouter, "\n  "))
	}
}

// TestAdminHandlerAuthCoverage asserts every admin mutation handler calls
// requireRole AND recordAudit/recordAuditOrError.
// Read-only handlers are exempt from the audit-write check (see exemption maps).
func TestAdminHandlerAuthCoverage(t *testing.T) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	dir := filepath.Dir(thisFile)

	// Handler files containing admin route handlers.
	handlerFiles := []string{
		filepath.Join(dir, "tenants.go"),
		filepath.Join(dir, "accounts.go"),
		filepath.Join(dir, "credentials.go"),
		filepath.Join(dir, "installs.go"),
		filepath.Join(dir, "audit.go"),
		filepath.Join(dir, "channel-types.go"),
		filepath.Join(dir, "messages.go"),
		filepath.Join(dir, "stream-health.go"),
	}

	// Dispatcher functions that delegate to typed sub-handlers — skip at top-level.
	// Auth is enforced in the sub-handlers called from these dispatchers.
	dispatchers := map[string]struct{}{
		"handleTenants":  {},
		"handleAccounts": {},
	}

	// Read-only handlers: protected by session (s.auth.Require on adminMux) and/or
	// requireRole, but do not record audit events on success (no mutation).
	auditExempt := map[string]struct{}{
		"handleListTenants":        {},
		"handleGetTenant":          {},
		"handleListAccounts":       {},
		"handleGetAccount":         {},
		"handleChannelTypes":       {},
		"handleCredentialMetadata": {},
		"handleWebhookInfo":        {},
		"handleListAudit":          {},
		"handleTailMessages":       {},
		"handleStreamHealth":       {},
	}

	// Handlers where role check is via session middleware (s.auth.Require on adminMux)
	// not per-handler requireRole. Pure reads accessible to all authenticated operators.
	roleExempt := map[string]struct{}{
		"handleListTenants":        {},
		"handleGetTenant":          {},
		"handleListAccounts":       {},
		"handleGetAccount":         {},
		"handleChannelTypes":       {},
		"handleCredentialMetadata": {},
		"handleWebhookInfo":        {},
		"handleTailMessages":       {},
		"handleStreamHealth":       {},
	}

	reRoleCall := regexp.MustCompile(`s\.requireRole\(`)
	reAuditCall := regexp.MustCompile(`s\.record(?:Audit|AuditOrError)\(`)

	for _, f := range handlerFiles {
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}

		funcs := splitFunctions(string(src))
		for name, body := range funcs {
			if !strings.HasPrefix(name, "handle") {
				continue
			}
			if _, isDispatcher := dispatchers[name]; isDispatcher {
				continue
			}

			if _, exempt := roleExempt[name]; !exempt {
				if !reRoleCall.MatchString(body) {
					t.Errorf("%s in %s: missing requireRole call", name, filepath.Base(f))
				}
			}

			if _, exempt := auditExempt[name]; !exempt {
				if !reAuditCall.MatchString(body) {
					t.Errorf("%s in %s: missing recordAudit/recordAuditOrError call (unaudited mutation)", name, filepath.Base(f))
				}
			}
		}
	}
}

// extractAdminRouterRoutes parses router.go to extract METHOD+normalized-path
// for all adminMux registrations.
func extractAdminRouterRoutes(src string) map[string]struct{} {
	routes := make(map[string]struct{})

	// Handlers registered without s.method() that accept multiple HTTP methods.
	multiMethod := map[string][]string{
		"/api/admin/tenants":  {"GET", "POST"},
		"/api/admin/accounts": {"GET", "PATCH"},
	}

	reHandle := regexp.MustCompile(`adminMux\.HandleFunc\(\s*"([^"]+)"`)
	reMethod := regexp.MustCompile(`s\.method\(http\.Method(Get|Post|Patch|Put|Delete)`)

	for _, line := range strings.Split(src, "\n") {
		pathMatch := reHandle.FindStringSubmatch(line)
		if pathMatch == nil {
			continue
		}
		rawPath := pathMatch[1]
		norm := normalizePath(rawPath)

		if methods, ok := multiMethod[rawPath]; ok {
			for _, m := range methods {
				routes[m+" "+norm] = struct{}{}
			}
			continue
		}

		if m := reMethod.FindStringSubmatch(line); m != nil {
			routes[strings.ToUpper(m[1])+" "+norm] = struct{}{}
		}
	}
	return routes
}

// splitFunctions splits Go source into a map of func-name → body text.
func splitFunctions(src string) map[string]string {
	result := make(map[string]string)
	reDef := regexp.MustCompile(`(?m)^func \([^)]+\) (\w+)\(`)

	matches := reDef.FindAllStringSubmatchIndex(src, -1)
	for i, m := range matches {
		name := src[m[2]:m[3]]
		start := m[0]
		end := len(src)
		if i+1 < len(matches) {
			end = matches[i+1][0]
		}
		result[name] = src[start:end]
	}
	return result
}

func isHTTPMethod(s string) bool {
	switch strings.ToUpper(s) {
	case "GET", "POST", "PUT", "PATCH", "DELETE":
		return true
	}
	return false
}
