SOURCES := $(shell find . -name '*.go')


cloudroutesync: $(SOURCES) 
	CGO_ENABLED=0 go build -o cloudroutesync -ldflags "-X main.version=$(VERSION) -extldflags -static" .


