
# Usage Guide

## 1. Go Lang 1.22.5 설치
1. [Go Lang 1.22.5](https://golang.org/dl/) 설치

## 2. Repository 클론
```sh
git clone https://github.com/testground/testground.git
git clone https://github.com/kmu-comnet/go-libp2p-testground.git
```

## 3. 파일 이동
```sh
mv go-libp2p-testground/composition.toml testground/composition.toml
```

## 4. 파일 수정
- `testground/pkg/build/docker_go.go` 파일 수정

  ```plaintext
  29: DefaultGoBuildBaseImage = "golang:1.22.5"
  583: ARG RUNTIME_IMAGE=busybox:1.36.1-glibc
  634-637: 
      RUN cd ${PLAN_DIR} && go mod download \
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
  653-655:
      RUN cd ${PLAN_DIR} && CGO_ENABLED=${CgoEnabled} GOOS=linux go build -o ${PLAN_DIR}/testplan.bin ${BUILD_TAGS} ${TESTPLAN_EXEC_PKG}
  ```

## 5. Testground 설치 및 실행
```sh
cd testground
make install
testground plan import --from /home/comnet/go-libp2p-testground
testground daemon
```

## 6. 새로운 쉘에서 실행
```sh
testground run composition -f composition.toml
```
