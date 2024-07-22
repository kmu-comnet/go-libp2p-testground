package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	logging "github.com/ipfs/go-log/v2"
	"github.com/libp2p/go-libp2p"
	dht "github.com/libp2p/go-libp2p-kad-dht"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	peer "github.com/libp2p/go-libp2p/core/peer"
	peerstore "github.com/libp2p/go-libp2p/core/peerstore"
	ma "github.com/multiformats/go-multiaddr"
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
		Level:  logging.LevelInfo,
		Stdout: true,
		Format: logging.JSONOutput,
	}
	logging.SetupLogging(logConfig)

	runenv.RecordMessage("before sync.MustBoundClient")
	client := initCtx.SyncClient
	netclient := initCtx.NetClient

	netConfig := &network.Config{
		Network: "default",

		Enable: true,
		Default: network.LinkShape{
			Latency:   100 * time.Millisecond,
			Bandwidth: 1 << 20, // 1Mib
		},
		CallbackState: "network-configured",
		RoutingPolicy: network.DenyAll,
	}

	runenv.RecordMessage("before netclient.MustConfigureNetwork")
	netclient.MustConfigureNetwork(ctx, netConfig)

	containerAddr, _ := netclient.GetDataNetworkIP()
	myAddr, _ := ma.NewMultiaddr(fmt.Sprintf("/ip4/%s/tcp/3000", containerAddr.To4().String()))
	host, _ := libp2p.New(libp2p.ListenAddrs(myAddr))
	runenv.RecordMessage("created libp2p host")

	dhtOpts := []dht.Option{
		dht.Mode(dht.ModeServer),
		dht.BucketSize(20),
	}

	_, err := dht.New(ctx, host, dhtOpts...)
	if err != nil {
		fmt.Printf("Failed initializing dht, error: %s\n", err)
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

		bootInfo, _ := tgSubscribe(ctx, client, runenv)
		bootstrap = bootInfo

		runenv.RecordMessage("received bootstrap info, adding address...")
		host.Peerstore().AddAddr(bootstrap.ID, bootstrap.Addrs[0], peerstore.PermanentAddrTTL)

		runenv.RecordMessage("all peers ready, connecting...")
		err := host.Connect(ctx, bootstrap)
		if err != nil {
			fmt.Printf("Failed connecting to %s, error: %s\n", bootstrap.ID, err)
		} else {
			fmt.Println("Connected to:", bootstrap.ID)
		}
	}

	gossipsub, _ := pubsub.NewGossipSub(ctx, host)
	topic, _ := gossipsub.Join(*topicNameFlag)
	runenv.RecordMessage("joined gossipsub topic")

	subscribe, err := topic.Subscribe()
	if err != nil {
		panic(err)
	}
	runenv.RecordMessage("subscribed gossipsub topic")
	printMessagesFrom(ctx, subscribe)

	if runenv.TestGroupID == "publisher" {
		seq, _ := client.SignalEntry(ctx, "ready-to-publish")
		if seq < 11 {
			go streamFileTo(ctx, topic, runenv.StringParam("small"))
			runenv.RecordMessage("published small file")
		} else if seq < 23 {
			go streamFileTo(ctx, topic, runenv.StringParam("medium"))
			runenv.RecordMessage("published medium file")
		} else {
			go streamFileTo(ctx, topic, runenv.StringParam("large"))
			runenv.RecordMessage("published large file")
		}
	}

	select {}
}

func streamFileTo(ctx context.Context, topic *pubsub.Topic, filePath string) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		fmt.Println("Error reading file:", err)
		return
	}
	if err := topic.Publish(ctx, data); err != nil {
		fmt.Println("### Publish error:", err)
	}
}

func printMessagesFrom(ctx context.Context, subscribing *pubsub.Subscription) {
	for {
		message, err := subscribing.Next(ctx)
		if err != nil {
			panic(err)
		}
		fmt.Println(message.ReceivedFrom, ": ", string(message.Message.Data))
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
