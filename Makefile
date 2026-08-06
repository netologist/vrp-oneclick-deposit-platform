.PHONY: proto tidy migrate up down run-all test lint build docs-serve docs-build

export PATH := $(PATH):$(HOME)/go/bin:$(shell go env GOPATH)/bin

MODULE_ROOT := github.com/netologist/vrp-oneclick-deposit-platform

proto:
	cd proto && buf generate
	cd gen && go mod tidy

tidy:
	cd pkg/shared && go mod tidy
	cd gen && go mod tidy
	@for svc in gateway merchant-svc consent-svc risk-svc ledger-svc bank-adapter payment-svc notification-svc; do \
		(cd services/$$svc && go mod tidy); \
	done

migrate:
	@command -v migrate >/dev/null || go install -tags 'postgres' github.com/golang-migrate/migrate/v4/cmd/migrate@v4.18.2
	migrate -path migrations/merchant -database "postgres://vrp:vrp@localhost:5432/merchant?sslmode=disable" up
	migrate -path migrations/consent  -database "postgres://vrp:vrp@localhost:5432/consent?sslmode=disable" up
	migrate -path migrations/payment  -database "postgres://vrp:vrp@localhost:5432/payment?sslmode=disable" up
	migrate -path migrations/ledger   -database "postgres://vrp:vrp@localhost:5432/ledger?sslmode=disable" up

up:
	docker compose up -d
	@echo "waiting for postgres..."
	@sleep 3
	@$(MAKE) migrate
	@echo "infra ready"

down:
	docker compose down

build:
	@for svc in gateway merchant-svc consent-svc risk-svc ledger-svc bank-adapter payment-svc notification-svc; do \
		echo "building $$svc..."; \
		(cd services/$$svc && go build -o ../../bin/$$svc ./cmd); \
	done

run-all: build
	@mkdir -p logs
	@./scripts/run-all.sh

test:
	cd pkg/shared && go test ./...
	@for svc in merchant-svc consent-svc risk-svc ledger-svc bank-adapter payment-svc; do \
		(cd services/$$svc && go test ./...); \
	done

lint:
	golangci-lint run ./...
docs-serve:
	NO_MKDOCS_2_WARNING=1 mkdocs serve

docs-build:
	NO_MKDOCS_2_WARNING=1 mkdocs build --strict
