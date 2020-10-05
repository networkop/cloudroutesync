SOURCES := $(shell find . -name '*.go')


cloudroutersync: $(SOURCES) 
	CGO_ENABLED=0 go build -o cloudroutersync -ldflags "-X main.version=$(VERSION) -extldflags -static" .


