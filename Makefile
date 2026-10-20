.PHONY: build run test test-vpn test-lan dev db-up db-down clean

BINARY=batter
GO=go

build:
	$(GO) build -o bin/$(BINARY) ./cmd/batter

run: build
	./bin/$(BINARY)

test:
	$(GO) test ./... -v

# Docker integration test of VPN profiles (builds the image; needs kernel
# WireGuard). See test/vpnit/vpn_test.go.
test-vpn:
	BATTER_DOCKER_IT=1 $(GO) test ./test/vpnit/ -v -count=1 -timeout 60m

# Docker integration test of the phone network (builds the image; needs
# kernel WireGuard and macvlan). See test/lanit/lan_test.go.
test-lan:
	BATTER_DOCKER_IT=1 $(GO) test ./test/lanit/ -v -count=1 -timeout 60m

dev:
	$(GO) run ./cmd/batter

db-up:
	docker compose up -d postgres

db-down:
	docker compose down

db-reset: db-down
	docker volume rm batter_pgdata || true
	$(MAKE) db-up

clean:
	rm -rf bin/

lint:
	golangci-lint run ./...

tidy:
	$(GO) mod tidy
