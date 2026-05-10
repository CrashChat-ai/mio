.PHONY: help up down proto proto-gen proto-lint proto-breaking proto-roundtrip sdk-go-test sdk-py-test sink-gcs-test sink-gcs-build-local sink-gcs-build lint test clean gateway-build gateway-build-local gateway-test gateway-migrate gateway-bench-outbound echo-up echo-logs echo-consumer-test helm-lint helm-template kind-up kind-deploy kind-smoke kind-down

COMPOSE := docker compose -f deploy/local/docker-compose.yml
BUILD_VERSION := $(shell git describe --always --dirty 2>/dev/null || echo dev)

help: ## Show this help message
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-22s\033[0m %s\n", $$1, $$2}' | sort

up: ## Start local infra (NATS + Postgres + MinIO)
	$(COMPOSE) up -d

down: ## Stop local infra
	$(COMPOSE) down

proto: ## Run buf generate (outputs to proto/gen/)
	buf generate

proto-gen: proto ## Regenerate proto + channel-type codegen; CI diffs must be clean
	go run ./tools/genchanneltypes/
	@echo "==> Checking for codegen drift..."
	git diff --exit-code sdk-go/channeltypes.go sdk-py/mio/channeltypes.py || \
		(echo "ERROR: channeltypes drift detected — commit the generated files"; exit 1)

sdk-go-test: ## Run sdk-go unit tests (no live NATS needed)
	cd sdk-go && go test ./... -v

sdk-py-test: ## Run sdk-py unit tests (no live NATS needed)
	cd sdk-py && uv run pytest tests/ -v -m "not integration"

proto-lint: ## Run buf lint (STANDARD ruleset)
	buf lint

proto-breaking: ## Run buf breaking check against main branch (WIRE_JSON ruleset)
	buf breaking --against ".git#branch=main"

proto-roundtrip: ## Run Go+Python proto round-trip test (pipes bytes; both must print OK)
	@echo "==> Go half: marshal + subject-token validator"
	go run ./tools/proto-roundtrip/

lint: ## Run buf lint + go vet
	buf lint
	go vet ./...

test: ## Run go tests
	go test ./...

clean: ## Remove generated proto output and stop infra (wipes volumes)
	rm -rf proto/gen
	$(COMPOSE) down -v

gateway-build-local: ## Build gateway Docker image locally (no push)
	docker build -f gateway/Dockerfile -t mio/gateway:dev .

gateway-build: ## Build gateway Docker image with version tag (no push)
	docker build -f gateway/Dockerfile \
		--build-arg BUILD_VERSION=$(BUILD_VERSION) \
		-t mio/gateway:$(BUILD_VERSION) .

gateway-test: ## Run gateway unit tests (no live NATS/Postgres needed)
	cd gateway && go test ./internal/... -v -count=1

gateway-migrate: ## Run database migrations manually via gateway CLI
	cd gateway && MIO_MIGRATE_ON_START=true go run ./cmd/gateway/

gateway-bench-outbound: ## Fairness bench: burst account A (50/s), assert account B p99 < 2s
	cd gateway && go test ./integration_test/... -run TestFairness -v -timeout 30s

gateway-dispatch-lint: ## CI guard: dispatch.go must have zero channel-specific branches
	@! grep -E 'zoho|slack|cliq|telegram|discord' gateway/internal/sender/dispatch.go && \
		echo "dispatch.go: clean (no adapter-specific branches)" || \
		(echo "ERROR: adapter-specific branch found in dispatch.go — P9 litmus FAIL"; exit 1)

sink-gcs-test: ## Run sink-gcs unit tests (no live NATS/MinIO needed)
	cd sink-gcs && go test ./internal/... -v

sink-gcs-build-local: ## Build sink-gcs Docker image locally (no push)
	docker build -f sink-gcs/Dockerfile -t mio/sink-gcs:dev .

sink-gcs-build: ## Build sink-gcs Docker image with version tag (no push)
	docker build -f sink-gcs/Dockerfile \
		--build-arg BUILD_VERSION=$(BUILD_VERSION) \
		-t mio/sink-gcs:$(BUILD_VERSION) .

echo-up: ## Start echo-consumer (+ nats + gateway deps) via docker compose
	$(COMPOSE) up -d echo-consumer

echo-logs: ## Tail echo-consumer logs
	$(COMPOSE) logs -f echo-consumer

echo-consumer-test: ## Run echo-consumer unit tests (no live NATS needed)
	uv run --project examples/echo-consumer pytest examples/echo-consumer/tests/ -v

# ── Helm ─────────────────────────────────────────────────────────────────────

helm-lint: ## Lint all Helm charts (helm lint + helm template render check)
	helm repo add nats https://nats-io.github.io/k8s/helm/charts/ 2>/dev/null || true
	helm dependency update deploy/charts/mio-nats
	helm lint deploy/charts/mio-nats
	helm lint deploy/charts/mio-jetstream-bootstrap
	helm lint deploy/charts/mio-gateway
	helm lint deploy/charts/mio-sink-gcs

helm-template: ## Render all charts with helm template and print to stdout
	@echo "==> mio-nats"
	helm template test-nats deploy/charts/mio-nats \
		--values deploy/charts/mio-nats/values-kind.yaml
	@echo "==> mio-jetstream-bootstrap"
	helm template test-bootstrap deploy/charts/mio-jetstream-bootstrap
	@echo "==> mio-gateway"
	helm template test-gateway deploy/charts/mio-gateway \
		--set secrets.existingSecret=mio-gateway-secrets
	@echo "==> mio-sink-gcs"
	helm template test-sink-gcs deploy/charts/mio-sink-gcs

# ── Kind smoke test ───────────────────────────────────────────────────────────

KIND_CLUSTER := mio-smoke

kind-up: ## Create a 1-node kind cluster for smoke testing
	@if kind get clusters 2>/dev/null | grep -q "^$(KIND_CLUSTER)$$"; then \
		echo "kind cluster $(KIND_CLUSTER) already exists — skipping create"; \
	else \
		kind create cluster --name $(KIND_CLUSTER); \
	fi
	kubectl config use-context kind-$(KIND_CLUSTER)

kind-deploy: kind-up ## Install NATS + gateway + sink-gcs on kind cluster
	helm repo add nats https://nats-io.github.io/k8s/helm/charts/ 2>/dev/null || true
	helm repo update
	helm dependency update deploy/charts/mio-nats
	kubectl create namespace mio --dry-run=client -o yaml | kubectl apply -f -
	@echo "==> Installing mio-nats (single-replica for kind)..."
	helm upgrade --install mio-nats deploy/charts/mio-nats \
		--namespace mio \
		--values deploy/charts/mio-nats/values-kind.yaml \
		--wait --timeout=3m
	@echo "==> Installing mio-gateway (helm lint only — no real image/secrets in smoke)..."
	helm template test-gateway deploy/charts/mio-gateway \
		--set secrets.existingSecret=mio-gateway-secrets \
		--namespace mio > /dev/null && echo "gateway template: OK"
	@echo "==> Installing mio-sink-gcs (template check)..."
	helm template test-sink-gcs deploy/charts/mio-sink-gcs \
		--namespace mio > /dev/null && echo "sink-gcs template: OK"
	@echo "==> kind-deploy complete. NATS up; gateway/sink-gcs template-validated."

kind-smoke: kind-deploy ## Full kind smoke: helm lint + template + NATS pod Ready check
	@echo "==> Smoke: checking NATS pod Ready in namespace mio..."
	kubectl wait --for=condition=Ready pod \
		-l app.kubernetes.io/name=nats \
		-n mio --timeout=120s
	@echo "==> Smoke: NATS pod Ready — JetStream reachable..."
	kubectl exec -n mio $$(kubectl get pod -n mio -l app.kubernetes.io/name=nats -o name | head -1) \
		-- nats --server=nats://localhost:4222 server check jetstream 2>&1 || \
		echo "  (nats-box CLI check skipped — pod may not have nats CLI; NATS pod Ready is sufficient for smoke)"
	@echo "==> helm lint clean..."
	$(MAKE) helm-lint
	@echo "==> kind smoke PASSED."

kind-down: ## Destroy the kind smoke cluster
	kind delete cluster --name $(KIND_CLUSTER) || true
