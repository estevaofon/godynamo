.PHONY: test race vet cover

# Unit tests only — NEVER hits real AWS (see test suite spec).
test:
	go test ./...

race:
	go test ./... -race

vet:
	go vet ./...

cover:
	go test ./... -coverprofile=coverage.out
	go tool cover -html=coverage.out
