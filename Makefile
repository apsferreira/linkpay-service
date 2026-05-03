.PHONY: run build test migrate docker-build

run:
	cd backend && go run ./cmd/server

build:
	cd backend && go build -o ../bin/linkpay-service ./cmd/server

test:
	cd backend && go test ./...

migrate:
	@for f in backend/migrations/*.up.sql; do \
		echo "Applying $$f..."; \
		psql "$$DATABASE_URL" -f "$$f"; \
	done

docker-build:
	docker build -t linkpay-service:local .

lint:
	cd backend && golangci-lint run ./...
