usage
1. go lang 1.16 설치
2. git clone https://github.com/testground/testground.git
3. git clone https://github.com/kmu-comnet/go-libp2p-testground.git
4. mv go-libp2p-testground/composition.toml testground/composition.toml
5. mv go-libp2p-testground/docker_go.go testground/pkg/build/docker_go.go
5. cd testground
6. testground plan import --from /home/comnet/go-libp2p-testground
7. testground daemon
8. 새로운 쉘에서 testground run composition -f composition.toml