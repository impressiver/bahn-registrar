.PHONY: clean build run doc fmt lint vet test

# Environment

PROJECT := mqtt-plumber
export PROJECT

# Prepend project .vendor directory to the system GOPATH so import will
# prioritize third party snapshots over common packages.
#GOPATH := ${PWD}/.vendor:${GOPATH}
#export GOPATH

default: build

clean:
	-e ./bin/${PROJECT} && rm ./bin/${PROJECT}

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

# vendor_clean:
#     rm -dRf ./.vendor/src

# # We have to set GOPATH to just the .vendor
# # directory to ensure that `go get` doesn't
# # update packages in our primary GOPATH instead.
# # This will happen if you already have the package
# # installed in GOPATH since `go get` will use
# # that existing location as the destination.
# vendor_get: vendor_clean
#     GOPATH=${PWD}/.vendor go get -d -u -v \
#     github.com/jpoehls/gophermail \
#     github.com/codegangsta/martini

# vendor_update: vendor_get
#     rm -rf `find ./.vendor/src -type d -name .git` \
#     && rm -rf `find ./.vendor/src -type d -name .hg` \
#     && rm -rf `find ./.vendor/src -type d -name .bzr` \
#     && rm -rf `find ./.vendor/src -type d -name .svn`
