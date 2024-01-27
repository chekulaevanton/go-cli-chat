GOCMD=go
GOBUILD=$(GOCMD) build

build-server:
	$(GOBUILD) -o bin/server cmd/server/main.go

build-client:
	$(GOBUILD) -o bin/client cmd/client/main.go
