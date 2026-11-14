export PATH := $(PATH):`go env GOPATH`/bin
export GO111MODULE=on
LDFLAGS := -s -w
FRP_COMPAT_BASELINE_COUNT ?= 8
FRP_COMPAT_FLOOR_VERSION ?= 0.61.0

.PHONY: frps frpc e2e-compatibility-smoke e2e-compatibility e2e-compatibility-floor

all: env fmt build

build: frps frpc

env:
	@go version

fmt:
	go fmt ./...

fmt-more:
	gofumpt -l -w .

gci:
	gci write -s standard -s default -s "prefix(github.com/fatedier/frp/)" ./

vet:
	go vet ./...

frps:
	env CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -tags "frps" -o bin/frps ./cmd/frps

frpc:
	env CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -tags "frpc" -o bin/frpc ./cmd/frpc

test: gotest

gotest:
	go test -v --cover ./cmd/...
	go test -v --cover ./client/...
	go test -v --cover ./server/...
	go test -v --cover ./pkg/...

e2e:
	./hack/run-e2e.sh

e2e-trace:
	DEBUG=true LOG_LEVEL=trace ./hack/run-e2e.sh

e2e-compatibility-smoke: build
	FRP_COMPAT_BASELINE_COUNT=1 ./hack/run-e2e-compatibility.sh

e2e-compatibility: build
	FRP_COMPAT_BASELINE_COUNT="$(FRP_COMPAT_BASELINE_COUNT)" ./hack/run-e2e-compatibility.sh

e2e-compatibility-floor: build
	FRP_COMPAT_BASELINE_VERSIONS="$(FRP_COMPAT_FLOOR_VERSION)" ./hack/run-e2e-compatibility.sh

e2e-compatibility-last-frpc:
	if [ ! -d "./lastversion" ]; then \
		TARGET_DIRNAME=lastversion ./hack/download.sh; \
	fi
	FRPC_PATH="`pwd`/lastversion/frpc" ./hack/run-e2e.sh
	rm -r ./lastversion

e2e-compatibility-last-frps:
	if [ ! -d "./lastversion" ]; then \
		TARGET_DIRNAME=lastversion ./hack/download.sh; \
	fi
	FRPS_PATH="`pwd`/lastversion/frps" ./hack/run-e2e.sh
	rm -r ./lastversion

alltest: vet gotest e2e
	
clean:
	rm -f ./bin/frpc
	rm -f ./bin/frps
	rm -rf ./lastversion
	rm -rf ./.cache
	rm -rf ./.compat
