package glt

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	logging "github.com/ipfs/go-log/v2"
	"github.com/libp2p/go-libp2p"
	dht "github.com/libp2p/go-libp2p-kad-dht"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/peerstore"
	drouting "github.com/libp2p/go-libp2p/p2p/discovery/routing"
	dutil "github.com/libp2p/go-libp2p/p2p/discovery/util"
	rcmgr "github.com/libp2p/go-libp2p/p2p/host/resource-manager"
	ma "github.com/multiformats/go-multiaddr"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/testground/sdk-go/run"
	"github.com/testground/sdk-go/runtime"
	"github.com/testground/sdk-go/sync"
)

// 현재 모든 test case에서 node라는 함수를 호출하여 실행하고 있습니다. 함수 “node”가 정의된 파일입니다.
// libp2p gossipsub에서 사용할 topic을 정의하고 있습니다.
var (
	topicNameFlag = flag.String("topicName", "comnet", "name of topic to join")
)

func node(runenv *runtime.RunEnv, initCtx *run.InitContext) error {
	flag.Parse()
	ctx := context.Background()

	// libp2p 로그 작성 모듈(ipfs/go-log/v2)을 설정합니다.
	// Testground 로그와 별개로 libp2p 코드베이스에서 배출하는 로그를 수집하고 파일에 쓰는 역할을 합니다.
	// 실험이 진행되는 동안 testground/data/outputs 경로에 libp2p.log 파일이 JSON 형식으로 작성되고, Stdout 여부에 따라 콘솔에도 출력됩니다.
	// 로그의 중요도에 따라 필터링이 가능한데, LevelDebug가 가장 상세하고 그 다음 수준은 Info 입니다.
	logging.Logger("libp2p")
	logConfig := logging.Config{
		File:   filepath.Join(runenv.TestOutputsPath, "libp2p.log"),
		Level:  logging.LevelDebug,
		Stdout: false,
		Format: logging.JSONOutput,
	}
	logging.SetupLogging(logConfig)

	// Testground 클라이언트(테스트를 주관하는 daemon, 테스트 시작 명령을 전달한 CLI client와 별개로
	// 프로그램 내부에서 Testground daemon과 소통하는 주체를 SyncClient, NetClient로 부릅니다.)를 initialize합니다.
	runenv.RecordMessage("before sync.MustBoundClient")
	client := initCtx.SyncClient
	netclient := initCtx.NetClient

	// testground netClient로부터 sync service에 쓰이는 것과 다른 ip(테스트 인스턴스들 간 통신에 쓰임) 주소를 얻어옵니다.
	// 그리고 libp2p에서 쓰이는 multiaddr 형식으로 변환합니다.
	containerAddr, _ := netclient.GetDataNetworkIP()
	multiAddr, _ := ma.NewMultiaddr(fmt.Sprintf("/ip4/%s/tcp/3000", containerAddr.To4().String()))

	// Prometheus로부터 오는 메트릭 수집 요청을 핸들링 할 수 있도록 서버를 설정합니다.
	go func() {
		http.Handle("/debug/metrics/prometheus", promhttp.Handler())
		panic(http.ListenAndServe(":5001", nil))
	}()

	// 리소스 관리자 관련 코드입니다. (TODO)
	rcmgr.MustRegisterWith(prometheus.DefaultRegisterer)
	str, _ := rcmgr.NewStatsTraceReporter()
	rmgr, _ := rcmgr.NewResourceManager(rcmgr.NewFixedLimiter(rcmgr.DefaultLimits.AutoScale()), rcmgr.WithTraceReporter(str))

	// libp2p 호스트 생성에 쓸 키 페어를 생성합니다. 암호화 방식은 선택할 수 있습니다.
	// 준비해둔 키페어, 멀티어드레스, 리소스 관리자를 명시하고 libp2p 호스트를 생성합니다.
	privKey, _, _ := crypto.GenerateKeyPair(crypto.RSA, 2048)
	node, _ := libp2p.New(libp2p.Identity(privKey), libp2p.ListenAddrs(multiAddr), libp2p.ResourceManager(rmgr))
	runenv.RecordMessage(fmt.Sprintf("created libp2p host, ID: %s", node.ID()))

	// composition.toml에서 설정한 test group id가 boot인 경우 자신의 ID와 multiaddr을
	// testground에서 제공하는 publish 기능을 통해 배포합니다.
	if runenv.TestGroupID == "boot" {
		myInfo := peer.AddrInfo{
			ID:    node.ID(),
			Addrs: node.Addrs(),
		}
		runenv.RecordMessage("publishing bootstrap info...")
		_ = tgPublish(client, myInfo)

		// boot 그룹 외 모든 그룹은 해당 내용을 subscribe하여 peerStore에 주소를 추가하고, 연결을 시도합니다.
	} else {
		tgBootstrap, _ := tgSubscribe(client, runenv)

		runenv.RecordMessage("received bootstrap info, adding address...")
		node.Peerstore().AddAddr(tgBootstrap.ID, tgBootstrap.Addrs[0], peerstore.PermanentAddrTTL)

		runenv.RecordMessage("all peers ready, connecting...")
		_ = node.Connect(ctx, tgBootstrap)
		runenv.RecordMessage(fmt.Sprintf("Connected to: %s", tgBootstrap.ID))
	}

	// 카뎀리아 dht를 구성할 옵션을 설정합니다.
	dhtOpts := []dht.Option{
		dht.Mode(dht.ModeServer),
		dht.BucketSize(20),
	}
	// 카뎀리아 dht를 생성합니다.
	kad, _ := dht.New(ctx, node, dhtOpts...)
	// kademlia 부트스트랩을 시작하고, gossipsub에서 kademlia 주소록을 쓸 수 있도록 합니다. (TODO)
	_ = kad.Bootstrap(ctx)
	routingDiscovery := drouting.NewRoutingDiscovery(kad)
	dutil.Advertise(ctx, routingDiscovery, *topicNameFlag)

	// libp2p 호스트에 gossipsub 프로토콜을 추가합니다. topic에 join합니다.
	gossipsub, _ := pubsub.NewGossipSub(ctx, node)
	topic, _ := gossipsub.Join(*topicNameFlag)
	runenv.RecordMessage("joined gossipsub topic")

	// topic을 구독합니다.
	subscribe, _ := topic.Subscribe()
	runenv.RecordMessage("subscribed gossipsub topic")

	runenv.RecordMessage("This is version 004")

	// test group id가 publisher인 경우 imagefile 파라미터(파일의 경로를 나타내는 문자열)를 불러와 배포합니다.
	if runenv.TestGroupID == "publisher" {
		go streamFileTo(ctx, topic, runenv.StringParam("imagefile"))
	}
	// gossipsub 메시지를 수신했다는 메시지를 콘솔에 출력하는 함수를 비동기적으로 실행합니다.
	go printMessagesFrom(ctx, subscribe, runenv)

	// test group id가 churn...인 경우 libp2p 호스트를 한번 종료하고, 다시 생성하는 함수를 실행합니다.
	if strings.HasPrefix(runenv.TestGroupID, "churn") {
		waitChannel := make(chan host.Host)
		go rebootHost(rmgr, node, privKey, runenv.IntParam("timer"), waitChannel, runenv)
		newNode := <-waitChannel

		dhtOpts := []dht.Option{
			dht.Mode(dht.ModeServer),
			dht.BucketSize(20),
		}

		kad, _ := dht.New(ctx, node, dhtOpts...)
		_ = kad.Bootstrap(ctx)
		routingDiscovery := drouting.NewRoutingDiscovery(kad)
		dutil.Advertise(ctx, routingDiscovery, *topicNameFlag)

		gossipsub, _ := pubsub.NewGossipSub(ctx, newNode)
		topic, _ := gossipsub.Join(*topicNameFlag)
		runenv.RecordMessage("joined gossipsub topic, again")

		subscribe, _ := topic.Subscribe()
		runenv.RecordMessage("subscribed gossipsub topic, again")

		// test group id가 churn-publisher인 경우 호스트 재부팅 후 textfile을 배포합니다.
		if runenv.TestGroupID == "churn-publisher" {
			go streamFileTo(ctx, topic, runenv.StringParam("textfile"))
		}
		go printMessagesFrom(ctx, subscribe, runenv)
	}

	// 전체 node 함수가 종료하지 않도록 무한 루프를 설정합니다.
	select {}
}

// 라우팅 테이블이 안정되기까지 기다리기 위해, 20초 가량 기다렸다가 파일을 publish 합니다.
func streamFileTo(ctx context.Context, topic *pubsub.Topic, filePath string) {
	time.Sleep(20 * time.Second)
	data, _ := os.ReadFile(filePath)
	_ = topic.Publish(ctx, data)
}

// 구독하고 있는 topic의 새로운 메시지를 수신하면 전달자의 ID와 함꼐 메시지를 받았다는 문자열을 출력합니다.
// testground와 연결된 influxdb의 카운터를 증가시킵니다.
func printMessagesFrom(ctx context.Context, subscribing *pubsub.Subscription, runenv *runtime.RunEnv) {
	for {
		message, _ := subscribing.Next(ctx)
		runenv.RecordMessage(fmt.Sprintf("Received message from: %s", message.ReceivedFrom))
		runenv.D().Counter("got.msg").Inc(1)
	}
}

// libp2p 호스트가 사용하고 있던 리소스 관리자, 키 페어 등을 받아 동일한 호스트를 새로 생성합니다. 대기하고 있는 waitChannel에 새 객체를 전달합니다.
func rebootHost(rcmgr network.ResourceManager, oldHost host.Host, privKey crypto.PrivKey, timer int, waitChannel chan host.Host, runenv *runtime.RunEnv) {

	peers := oldHost.Peerstore()
	addrs := oldHost.Addrs()

	time.Sleep(time.Duration(timer) * time.Second)
	oldHost.Close()
	runenv.RecordMessage("closed libp2p host")

	time.Sleep(time.Duration(timer) / 2 * time.Second)
	newHost, _ := libp2p.New(libp2p.Identity(privKey), libp2p.ListenAddrs(addrs[0]), libp2p.Peerstore(peers), libp2p.ResourceManager(rcmgr))
	runenv.RecordMessage(fmt.Sprintf("created libp2p host, again, ID: %s", newHost.ID()))

	waitChannel <- newHost
}

// testground에서 제공하는 publish 기능을 사용하는 함수입니다.
func tgPublish(client sync.Client, payload peer.AddrInfo) error {
	subCtx := context.Background()

	tgTopic := sync.NewTopic("bootstrap", peer.AddrInfo{})
	_, err := client.Publish(subCtx, tgTopic, payload)
	return err
}

// testground에서 제공하는 subscribe 기능을 사용하는 함수입니다.
// 내용을 수신하는데 성공하면 influxdb의 카운터를 증가시킵니다.
func tgSubscribe(client sync.Client, runenv *runtime.RunEnv) (peer.AddrInfo, error) {
	subCtx := context.Background()

	tgTopic := sync.NewTopic("bootstrap", peer.AddrInfo{})
	tgChannel := make(chan peer.AddrInfo, 1)
	_, _ = client.Subscribe(subCtx, tgTopic, tgChannel)

	tgMessage := <-tgChannel
	runenv.D().Counter("got.info").Inc(1)

	return tgMessage, nil
}
