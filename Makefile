TAG := $(shell git describe --tags --always --dirty)

all: build

version:
	@echo $(TAG)

build:
	go test -cover ./...
	go build  -v  ./...

all:
	for d in . storetest benchmarks providers/* middleware/*; do (cd "$d" && go test ./...); done 

update:
	go get -u ./...
