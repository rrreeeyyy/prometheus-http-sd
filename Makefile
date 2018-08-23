VERSION = 0.1.0
COMMIT = $(shell git describe --always)
GOFILES_NOVENDOR = $(shell find . -type f -name '*.go' -not -path "./vendor/*")

default: build

# build generate binary on './bin' directory.
build:
	go build -ldflags "-X main.GitCommit=$(COMMIT)" -o bin/prometheus-http-sd .

buildx:
	gox -ldflags "-X main.GitCommit=$(COMMIT)" -output "bin/v$(VERSION)/{{.Dir}}_{{.OS}}_{{.Arch}}" -arch "amd64" -os "linux darwin" .

lint:
	golint ${GOFILES_NOVENDOR}

vet:
	go vet -v ${GOFILES_NOVENDOR}

test:
	go test -v ${GOFILES_NOVENDOR}

fmt:
	gofmt -l -w ${GOFILES_NOVENDOR}

release: buildx
	git tag v$(VERSION)
	git push origin v$(VERSION)
	ghr -u rrreeeyyy v$(VERSION) bin/v$(VERSION)/

dep:
	dep ensure
	dep status
