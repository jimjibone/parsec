.PHONY: build web server dev-server dev-web check test clean

DATA ?= parsec-data

build: web server

web: web/node_modules
	cd web && npm run build

# Reinstall when either file changes; a lockfile-only change (e.g. a pull
# that adds a dependency) would otherwise leave node_modules stale.
web/node_modules: web/package.json web/package-lock.json
	cd web && npm install --no-audit --no-fund
	touch web/node_modules

server:
	go build -ldflags "-X parsec/internal/version.BuildDate=$$(date -u +%Y-%m-%dT%H:%M:%SZ)" -o parsec ./cmd/parsec

# Development: run both in separate terminals, then open http://localhost:5173
dev-server:
	go run ./cmd/parsec --data $(DATA)

dev-web: web/node_modules
	cd web && npm run dev

check: web/node_modules
	go vet ./...
	cd web && npm run check

test: web/node_modules
	go test ./...
	cd web && npm test

clean:
	rm -f parsec
	find internal/webui/dist -mindepth 1 ! -name .gitkeep -delete
