SOURCES := $(shell find . -name '*.go')


cloudrouter: $(SOURCES) 
	CGO_ENABLED=0 go build -o cloudprober -ldflags "-X main.version=$(VERSION) -extldflags -static" ./cmd/main.go


