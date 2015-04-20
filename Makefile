.PHONY: build run doc fmt lint vet test clean vendor_shrinkwrap

# Environment

PROJECT := mqtt-plumber
export PROJECT

# Prepend project .vendor directory to the system GOPATH so import will
# prioritize third party snapshots over common packages.
GOPATH := ${PWD}/.vendor:`godep path`:${GOPATH}
export GOPATH

default: build

build: clean vet
	go build -v -o ./bin/${PROJECT} ./${PROJECT}.go

run: build
	./bin/${PROJECT}

doc:
  godoc -http=:6060 -index

# http://golang.org/cmd/go/#hdr-Run_gofmt_on_package_sources
fmt:
	go fmt ./...

# https://github.com/golang/lint
# go get github.com/golang/lint/golint
lint:
	golint ./

# http://godoc.org/code.google.com/p/go.tools/cmd/vet
# go get code.google.com/p/go.tools/cmd/vet
vet:
	go vet ./...

test:
	go test ./...

clean: vendor_clean
	if [ -e ./bin/${PROJECT} ] ; then rm ./bin/${PROJECT} ; fi

vendor_clean:
	rm -dRf ./vendor

vendor_shrinkwrap: vendor_clean
		cp -Rf ./Godeps/_workspace/ ./.vendor && \
		rm -rf `find ./.vendor/src -type d -name .git` \
		&& rm -rf `find ./.vendor/src -type d -name .hg` \
		&& rm -rf `find ./.vendor/src -type d -name .bzr` \
		&& rm -rf `find ./.vendor/src -type d -name .svn`
