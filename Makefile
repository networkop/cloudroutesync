SOURCES := $(shell find . -name '*.go')
DOCKER_IMAGE ?= networkop/cloudroutesync

cloudroutesync: $(SOURCES) 
	CGO_ENABLED=0 go build -o cloudroutesync -ldflags "-X main.version=$(VERSION) -extldflags -static" .


docker_build: cloudroutesync Dockerfile
	docker build -t $(DOCKER_IMAGE)  .

docker_push:
	docker push $(DOCKER_IMAGE)