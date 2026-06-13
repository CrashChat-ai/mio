import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { dirname, join } from "node:path";
import { parse } from "yaml";

const here = dirname(fileURLToPath(import.meta.url));
const specPath = join(here, "..", "..", "api-contracts", "openapi.yaml");
const routerPath = join(here, "..", "..", "internal", "rest", "router.go");

const spec = parse(readFileSync(specPath, "utf8"));
const router = readFileSync(routerPath, "utf8");

const specRoutes = new Set();
for (const [path, item] of Object.entries(spec.paths ?? {})) {
  for (const method of Object.keys(item)) {
    if (["get", "post", "patch", "put", "delete"].includes(method)) {
      specRoutes.add(`${method.toUpperCase()} ${path}`);
    }
  }
}

const routerRoutes = new Set();
const handle = /(?:mux|adminMux)\.HandleFunc\(\s*"([^"]+)"\s*,\s*([\s\S]*?)\)\n/g;
const methodGuard = /s\.method\(http\.Method(Get|Post|Patch|Put|Delete)/;
const explicitMethods = {
  "/api/admin/tenants": ["GET", "POST"],
  "/api/admin/accounts": ["GET", "PATCH"],
};

for (const m of router.matchAll(handle)) {
  const path = m[1];
  const body = m[2];
  if (path === "/") continue;
  if (explicitMethods[path]) {
    for (const method of explicitMethods[path]) routerRoutes.add(`${method} ${path}`);
    continue;
  }
  const guard = methodGuard.exec(body);
  if (guard) {
    routerRoutes.add(`${guard[1].toUpperCase()} ${path}`);
    continue;
  }
  if (/HandleLogin/.test(body) || /HandleCallback/.test(body)) {
    routerRoutes.add(`GET ${path}`);
  } else if (/HandleLogout/.test(body)) {
    routerRoutes.add(`POST ${path}`);
  } else if (path === "/healthz") {
    routerRoutes.add(`GET ${path}`);
  } else {
    throw new Error(`contract-coverage: cannot infer method for route ${path}`);
  }
}

const missingInSpec = [...routerRoutes].filter((r) => !specRoutes.has(r)).sort();
const orphanInSpec = [...specRoutes].filter((r) => !routerRoutes.has(r)).sort();

if (missingInSpec.length || orphanInSpec.length) {
  if (missingInSpec.length) {
    console.error("Routes registered in router.go but missing from openapi.yaml:");
    for (const r of missingInSpec) console.error(`  - ${r}`);
  }
  if (orphanInSpec.length) {
    console.error("Paths in openapi.yaml with no matching router.go route:");
    for (const r of orphanInSpec) console.error(`  - ${r}`);
  }
  process.exit(1);
}

console.log(`contract coverage OK: ${routerRoutes.size} routes <-> spec`);
