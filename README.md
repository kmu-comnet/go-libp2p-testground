
# Usage Guide

## 1. Go Lang 1.22.5 설치
1. [Go Lang 1.22.5](https://golang.org/dl/) 설치

## 2. Repository 클론

```sh
git clone https://github.com/testground/testground.git
git clone https://github.com/kmu-comnet/go-libp2p-testground.git
```

## 3. 파일 이동
- 실행할 테스트 케이스의 이름과 인스턴스 갯수를 go-libp2p-testground/manifest.toml 참고하여 수정 후 testground/composition.toml 위치로 이동

```sh
mv go-libp2p-testground/composition.toml testground/composition.toml
```

## 4. 파일 수정
- docker:go 빌더의 경우 내부적으로 도커파일을 자동 생성하여 빌드하는데, 언어 버전이 맞지 않으므로 `testground/pkg/build/docker_go.go` 파일 내의 도커파일 양식을 수정

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
      && go get github.com/testground/sdk-go/network \
      && go get github.com/testground/sdk-go/run \
      && go get github.com/testground/sdk-go/runtime \
      && go get github.com/testground/sdk-go/sync
  653-655:
      RUN cd ${PLAN_DIR} && CGO_ENABLED=${CgoEnabled} GOOS=linux go build -o ${PLAN_DIR}/testplan.bin ${BUILD_TAGS} ${TESTPLAN_EXEC_PKG}
  ```

## 5. Testground 빌드 및 실행

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

## 7. 결과 확인
- testground/data 경로 아래 테스트 런 식별자를 이름으로 하는 디렉토리가 생성되고 composition.toml 에서 지정한 그룹별, 인스턴스 개별 output 디렉토리가 생성되므로 확인
- testground 로그는 run.out, libp2p 로그는 libp2p.log 파일 이름을 가짐
- leveldb 및 influxdb 컨테이너로 전송된 데이터는 아직 확인하지 못함
