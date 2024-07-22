usage
1. go lang 1.22.5 설치
2. git clone https://github.com/testground/testground.git
3. git clone https://github.com/kmu-comnet/go-libp2p-testground.git
4. mv go-libp2p-testground/composition.toml testground/composition.toml
5. testground/pkg/build/docker_go.go 파일 수정
    29:         DefaultGoBuildBaseImage = "golang:1.22.5"
    583:        ARG RUNTIME_IMAGE=busybox:1.36.1-glibc
    634~637:    RUN cd ${PLAN_DIR} && go mod download \
                && go get github.com/libp2p/go-libp2p \
                && go get github.com/libp2p/go-libp2p-kad-dht \
                && go get github.com/libp2p/go-libp2p-pubsub \
                && go get github.com/libp2p/go-libp2p/core/host \
                && go get github.com/libp2p/go-libp2p/core/peer \
                && go get github.com/libp2p/go-libp2p/p2p/discovery/routing \
                && go get github.com/libp2p/go-libp2p/p2p/discovery/util \
                && go get github.com/testground/sdk-go/network \
                && go get github.com/testground/sdk-go/run \
                && go get github.com/testground/sdk-go/runtime \
                && go get github.com/testground/sdk-go/sync
    653~655:    RUN cd ${PLAN_DIR} && CGO_ENABLED=${CgoEnabled} GOOS=linux go build -o ${PLAN_DIR}/testplan.bin ${BUILD_TAGS} ${TESTPLAN_EXEC_PKG}
6. cd testground
7. make install
8. testground plan import --from /home/comnet/go-libp2p-testground
9. testground daemon
10. 새로운 쉘에서 testground run composition -f composition.toml