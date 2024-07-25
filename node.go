package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"

	logging "github.com/ipfs/go-log/v2"
	"github.com/libp2p/go-libp2p"
	dht "github.com/libp2p/go-libp2p-kad-dht"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/peerstore"
	drouting "github.com/libp2p/go-libp2p/p2p/discovery/routing"
	dutil "github.com/libp2p/go-libp2p/p2p/discovery/util"
	rcmgr "github.com/libp2p/go-libp2p/p2p/host/resource-manager"
	ma "github.com/multiformats/go-multiaddr"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/testground/sdk-go/network"
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

	netConfig := &network.Config{
		Network: "default",

		Enable: true,
		Default: network.LinkShape{
			Latency:   time.Duration(myLatency) * time.Millisecond,
			Bandwidth: 1 << myBandwidth,
		},
		CallbackState: "network-configured",
		RoutingPolicy: network.DenyAll,
	}

	runenv.RecordMessage("before netclient.MustConfigureNetwork")
	netclient.MustConfigureNetwork(ctx, netConfig)

	http.Handle("/debug/metrics/prometheus", promhttp.Handler())
	rcmgr.MustRegisterWith(prometheus.DefaultRegisterer)

	str, err := rcmgr.NewStatsTraceReporter()
	if err != nil {
		panic(err)
	}

	rmgr, err := rcmgr.NewResourceManager(rcmgr.NewFixedLimiter(rcmgr.DefaultLimits.AutoScale()), rcmgr.WithTraceReporter(str))
	if err != nil {
		panic(err)
	}

	containerAddr, err := netclient.GetDataNetworkIP()
	if err != nil {
		panic(err)
	}

	myAddr, err := ma.NewMultiaddr(fmt.Sprintf("/ip4/%s/tcp/3000", containerAddr.To4().String()))
	if err != nil {
		panic(err)
	}

	host, err := libp2p.New(libp2p.ListenAddrs(myAddr), libp2p.ResourceManager(rmgr))
	if err != nil {
		panic(err)
	}
	runenv.RecordMessage("created libp2p host")

	dhtOpts := []dht.Option{
		dht.Mode(dht.ModeServer),
		dht.BucketSize(20),
	}

	kad, err := dht.New(ctx, host, dhtOpts...)
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
			ID:    host.ID(),
			Addrs: host.Addrs(),
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
		host.Peerstore().AddAddr(bootstrap.ID, bootstrap.Addrs[0], peerstore.PermanentAddrTTL)

		runenv.RecordMessage("all peers ready, connecting...")
		err = host.Connect(ctx, bootstrap)
		if err != nil {
			fmt.Printf("Failed connecting to %s, error: %s\n", bootstrap.ID, err)
		} else {
			fmt.Println("Connected to:", bootstrap.ID)
		}
	}

	routingDiscovery := drouting.NewRoutingDiscovery(kad)
	dutil.Advertise(ctx, routingDiscovery, *topicNameFlag)

	gossipsub, err := pubsub.NewGossipSub(ctx, host)
	if err != nil {
		panic(err)
	}

	topic, err := gossipsub.Join(*topicNameFlag)
	if err != nil {
		panic(err)
	}
	runenv.RecordMessage("joined gossipsub topic")

	if runenv.TestGroupID == "publisher" {
		go streamFileTo(ctx, topic, runenv.StringParam("file"))
	}

	subscribe, err := topic.Subscribe()
	if err != nil {
		panic(err)
	}
	runenv.RecordMessage("subscribed gossipsub topic")
	printMessagesFrom(ctx, subscribe, runenv)

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
		fmt.Println("Received message from: ", message.ReceivedFrom)
		runenv.D().Counter("got.msg").Inc(1)
	}
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
	_, err := client.Subscribe(subCtx, tgTopic, tgChannel)
	if err != nil {
		panic(err)
	}

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
