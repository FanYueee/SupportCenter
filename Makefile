.PHONY: generate assets build test preview

generate:
	go run github.com/a-h/templ/cmd/templ@v0.3.1020 generate
assets:
	npm ci
	npm run build
build: generate assets
	go build -o bin/server ./cmd/server
	go build -o bin/admin ./cmd/admin
test:
	go test ./...
preview:
	go run ./cmd/preview
