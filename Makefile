.PHONY: build run doc fmt lint vet test clean clean_vendor get_vendor save_vendor restore_vendor prune_vendor

# Environment

PROJECT := mqtt-plumber
export PROJECT

# Prepend project .vendor directory to the system GOPATH so import will
# prioritize third party snapshots over common packages.
GOPATH := ${PWD}/.vendor:${GOPATH}
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

clean:
	if [ -e ./bin/${PROJECT} ] ; then rm ./bin/${PROJECT} ; fi

clean_vendor:
		rm -dRf ./.vendor/src

# Set GOPATH to just the .vendor directory to ensure that `go get` will not
# update packages in the primary $GOPATH. This happens if the package is already
# installed in $GOPATH.
get_vendor: clean_vendor
		GOPATH=${PWD}/.vendor go get -d -u -v \
		github.com/influxdb/influxdb/client

save_vendor:
		GOPATH=${PWD}/.vendor godep save

restore_vendor: clean_vendor
		GOPATH=${PWD}/.vendor godep restore

prune_vendor:
		rm -rf `find ./.vendor/src -type d -name .git` \
		&& rm -rf `find ./.vendor/src -type d -name .hg` \
		&& rm -rf `find ./.vendor/src -type d -name .bzr` \
		&& rm -rf `find ./.vendor/src -type d -name .svn`
