usage
1. go lang 1.22.5 설치
2. git clone https://github.com/testground/testground.git
3. git clone https://github.com/kmu-comnet/go-libp2p-testground.git
4. mv go-libp2p-testground/composition.toml testground/composition.toml
5. docker_go.go 파일 상단의 build exclude 태그 삭제
6. mv go-libp2p-testground/docker_go.go testground/pkg/build/docker_go.go
7. cd testground
8. testground plan import --from /home/comnet/go-libp2p-testground
9. testground daemon
10. 새로운 쉘에서 testground run composition -f composition.toml