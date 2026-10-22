SHELL := /bin/sh

GO      ?= go
NPM     ?= npm
PORT    ?= 8080
BINARY  := bin/server
WEB     := web

.DEFAULT_GOAL := help

.PHONY: help
help: ## Affiche cette aide
	@printf '\nRandomChat — cibles disponibles :\n\n'
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) \
		| awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-16s\033[0m %s\n", $$1, $$2}'
	@printf '\nDémarrage rapide : make deps && make build && make run\n\n'

# ----------------------------------------------------------------- deps ---

.PHONY: deps
deps: deps-go deps-web ## Installe toutes les dépendances

.PHONY: deps-go
deps-go: ## Télécharge les modules Go
	$(GO) mod download

.PHONY: deps-web
deps-web: ## Installe les dépendances npm
	cd $(WEB) && $(NPM) install --include=dev

# ---------------------------------------------------------------- build ---

.PHONY: build
build: build-web build-api ## Compile le frontend puis le backend

.PHONY: build-api
build-api: ## Compile le binaire du serveur
	$(GO) build -trimpath -o $(BINARY) ./cmd/api

.PHONY: build-web
build-web: ## Compile le bundle React
	cd $(WEB) && $(NPM) run build

# ------------------------------------------------------------------ dev ---

.PHONY: dev-api
dev-api: ## Lance le serveur Go (port 8080)
	ADDR=:$(PORT) $(GO) run ./cmd/api

.PHONY: dev-web
dev-web: ## Lance Vite avec proxy vers le backend
	cd $(WEB) && $(NPM) run dev

# ---------------------------------------------------------------- tests ---

.PHONY: test
test: ## Lance la suite de tests Go
	$(GO) test ./...

.PHONY: test-race
test-race: ## Lance les tests avec le détecteur de courses
	$(GO) test -race -count=1 ./...

.PHONY: fmt
fmt: ## Formate le code Go
	$(GO) fmt ./...

.PHONY: vet
vet: ## Analyse statique Go
	$(GO) vet ./...

.PHONY: typecheck
typecheck: ## Vérifie les types TypeScript
	cd $(WEB) && $(NPM) run typecheck

.PHONY: check
check: vet test typecheck ## Contrôles complets avant commit

# ------------------------------------------------------------------ run ---

.PHONY: run
run: build ## Compile puis sert le SPA depuis le binaire Go
	./$(BINARY)

# --------------------------------------------------------------- docker ---

.PHONY: docker-build
docker-build: ## Construit l'image de production
	docker build -f deploy/Dockerfile -t random-chat:local .

.PHONY: docker-up
docker-up: ## Démarre l'API et PostgreSQL
	docker compose -f deploy/compose.yaml up -d --build

.PHONY: docker-up-turn
docker-up-turn: ## Démarre aussi le relais TURN
	docker compose -f deploy/compose.yaml --profile turn up -d --build

.PHONY: docker-down
docker-down: ## Arrête la pile Docker
	docker compose -f deploy/compose.yaml down

# --------------------------------------------------------------- housekeeping

.PHONY: clean
clean: ## Supprime les artefacts de build
	rm -rf bin $(WEB)/dist $(WEB)/node_modules
