package main

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
	tgnetwork "github.com/testground/sdk-go/network"
	"github.com/testground/sdk-go/run"
	"github.com/testground/sdk-go/runtime"
	"github.com/testground/sdk-go/sync"
)

var (
	topicNameFlag = flag.String("topicName", "comnet", "name of topic to join")
)

func node(runenv *runtime.RunEnv, initCtx *run.InitContext) error {
	flag.Parse()
	ctx := context.Background()

	logging.Logger("libp2p")
	logConfig := logging.Config{
		File:   filepath.Join(runenv.TestOutputsPath, "libp2p.log"),
		Level:  logging.LevelDebug,
		Stdout: false,
		Format: logging.JSONOutput,
	}
	logging.SetupLogging(logConfig)

	runenv.RecordMessage("before sync.MustBoundClient")
	client := initCtx.SyncClient
	netclient := initCtx.NetClient

	myLatency := runenv.IntParam("latency")
	myBandwidth := runenv.IntParam("bandwidth")

	if runenv.TestGroupID == "better" {
		myLatency = runenv.IntParam("better_latency")
		myBandwidth = runenv.IntParam("better_bandwidth")
	}

	netConfig := &tgnetwork.Config{
		Network: "default",

		Enable: true,
		Default: tgnetwork.LinkShape{
			Latency:   time.Duration(myLatency) * time.Millisecond,
			Bandwidth: 1 << myBandwidth,
		},
		CallbackState: "network-configured",
		RoutingPolicy: tgnetwork.DenyAll,
	}

	runenv.RecordMessage("before netclient.MustConfigureNetwork")
	netclient.MustConfigureNetwork(ctx, netConfig)

	containerAddr, _ := netclient.GetDataNetworkIP()
	multiAddr, _ := ma.NewMultiaddr(fmt.Sprintf("/ip4/%s/tcp/3000", containerAddr.To4().String()))

	go func() {
		http.Handle("/debug/metrics/prometheus", promhttp.Handler())
		panic(http.ListenAndServe(":5001", nil))
	}()
	rcmgr.MustRegisterWith(prometheus.DefaultRegisterer)

	str, err := rcmgr.NewStatsTraceReporter()
	if err != nil {
		panic(err)
	}

	rmgr, err := rcmgr.NewResourceManager(rcmgr.NewFixedLimiter(rcmgr.DefaultLimits.AutoScale()), rcmgr.WithTraceReporter(str))
	if err != nil {
		panic(err)
	}

	privKey, _, _ := crypto.GenerateKeyPair(crypto.RSA, 2048)

	node, err := libp2p.New(libp2p.Identity(privKey), libp2p.ListenAddrs(multiAddr), libp2p.ResourceManager(rmgr))
	if err != nil {
		panic(err)
	}
	runenv.RecordMessage(fmt.Sprintf("created libp2p host, ID: %s", node.ID()))

	dhtOpts := []dht.Option{
		dht.Mode(dht.ModeServer),
		dht.BucketSize(20),
	}

	kad, err := dht.New(ctx, node, dhtOpts...)
	if err != nil {
		panic(err)
	}

	err = kad.Bootstrap(ctx)
	if err != nil {
		panic(err)
	}

	var bootstrap peer.AddrInfo

	if runenv.TestGroupID == "boot" {
		myInfo := peer.AddrInfo{
			ID:    node.ID(),
			Addrs: node.Addrs(),
		}
		runenv.RecordMessage("publishing bootstrap info...")
		err := tgPublish(ctx, client, myInfo)
		if err != nil {
			return err
		}
	} else {

		bootInfo, err := tgSubscribe(ctx, client, runenv)
		if err != nil {
			panic(err)
		}

		bootstrap = bootInfo

		runenv.RecordMessage("received bootstrap info, adding address...")
		node.Peerstore().AddAddr(bootstrap.ID, bootstrap.Addrs[0], peerstore.PermanentAddrTTL)

		runenv.RecordMessage("all peers ready, connecting...")
		err = node.Connect(ctx, bootstrap)
		if err != nil {
			fmt.Printf("Failed connecting to %s, error: %s\n", bootstrap.ID, err)
		} else {
			runenv.RecordMessage(fmt.Sprintf("Connected to: %s", bootstrap.ID))
		}
	}

	routingDiscovery := drouting.NewRoutingDiscovery(kad)
	dutil.Advertise(ctx, routingDiscovery, *topicNameFlag)

	gossipsub, err := pubsub.NewGossipSub(ctx, node)
	if err != nil {
		panic(err)
	}

	topic, err := gossipsub.Join(*topicNameFlag)
	if err != nil {
		panic(err)
	}
	runenv.RecordMessage("joined gossipsub topic")

	subscribe, err := topic.Subscribe()
	if err != nil {
		panic(err)
	}
	runenv.RecordMessage("subscribed gossipsub topic")

	if runenv.TestGroupID == "publisher" {
		go streamFileTo(ctx, topic, runenv.StringParam("imagefile"))
	} else {
		go printMessagesFrom(ctx, subscribe, runenv)
	}

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

		gossipsub, _ := pubsub.NewGossipSub(ctx, newNode)
		topic, _ := gossipsub.Join(*topicNameFlag)
		runenv.RecordMessage("joined gossipsub topic, again")

		subscribe, _ := topic.Subscribe()
		runenv.RecordMessage("subscribed gossipsub topic, again")

		routingDiscovery := drouting.NewRoutingDiscovery(kad)
		dutil.Advertise(ctx, routingDiscovery, *topicNameFlag)

		if runenv.TestGroupID == "churn-publisher" {
			go streamFileTo(ctx, topic, runenv.StringParam("textfile"))
		} else {
			go printMessagesFrom(ctx, subscribe, runenv)
		}
	}

	select {}
}

func streamFileTo(ctx context.Context, topic *pubsub.Topic, filePath string) {
	time.Sleep(20 * time.Second)
	data, err := os.ReadFile(filePath)
	if err != nil {
		fmt.Println("Error reading file:", err)
		return
	}
	if err := topic.Publish(ctx, data); err != nil {
		fmt.Println("### Publish error:", err)
	}
}

func printMessagesFrom(ctx context.Context, subscribing *pubsub.Subscription, runenv *runtime.RunEnv) {
	for {
		message, err := subscribing.Next(ctx)
		if err != nil {
			panic(err)
		}
		runenv.RecordMessage(fmt.Sprintf("Received message from: %s", message.ReceivedFrom))
		runenv.D().Counter("got.msg").Inc(1)
	}
}

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

func tgPublish(ctx context.Context, client sync.Client, payload peer.AddrInfo) error {
	subCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	tgTopic := sync.NewTopic("bootstrap", peer.AddrInfo{})
	_, err := client.Publish(subCtx, tgTopic, payload)
	return err
}

func tgSubscribe(ctx context.Context, client sync.Client, runenv *runtime.RunEnv) (peer.AddrInfo, error) {
	subCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	tgTopic := sync.NewTopic("bootstrap", peer.AddrInfo{})
	tgChannel := make(chan peer.AddrInfo, 1)
	_, _ = client.Subscribe(subCtx, tgTopic, tgChannel)

	bootInfo := peer.AddrInfo{}

	select {
	case tgMessage := <-tgChannel:
		bootInfo = peer.AddrInfo(tgMessage)
	case <-ctx.Done():
		return bootInfo, ctx.Err()
	}
	runenv.D().Counter("got.info").Inc(1)

	return bootInfo, nil
}
