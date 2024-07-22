package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/libp2p/go-libp2p"
	dht "github.com/libp2p/go-libp2p-kad-dht"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/libp2p/go-libp2p/core/host"
	peer "github.com/libp2p/go-libp2p/core/peer"
	drouting "github.com/libp2p/go-libp2p/p2p/discovery/routing"
	dutil "github.com/libp2p/go-libp2p/p2p/discovery/util"
	ma "github.com/multiformats/go-multiaddr"
	"github.com/testground/sdk-go/network"
	"github.com/testground/sdk-go/run"
	"github.com/testground/sdk-go/runtime"
	"github.com/testground/sdk-go/sync"
)

var (
	topicNameFlag = flag.String("topicName", "comnet", "name of topic to join")
)

type nodeInfo struct {
	ID   peer.ID
	Addr ma.Multiaddr
}

func node(runenv *runtime.RunEnv, initCtx *run.InitContext) error {
	flag.Parse()
	ctx := context.Background()

	runenv.RecordMessage("before sync.MustBoundClient")
	client := initCtx.SyncClient
	netclient := initCtx.NetClient

	config := &network.Config{
		Network: "default",

		Enable: true,
		Default: network.LinkShape{
			Latency:   100 * time.Millisecond,
			Bandwidth: 1 << 20, // 1Mib
		},
		CallbackState: "network-configured",
		RoutingPolicy: network.DenyAll,
	}

	ready := sync.State("network-configured")
	runenv.RecordMessage("before netclient.MustConfigureNetwork")
	netclient.MustConfigureNetwork(ctx, config)
	seq, err := client.SignalEntry(ctx, ready)

	containerAddr, _ := netclient.GetDataNetworkIP()
	hostAddr, _ := ma.NewMultiaddr(fmt.Sprintf("/ip4/%s/tcp/3000", containerAddr.To4().String()))
	host, _ := libp2p.New(libp2p.ListenAddrs(hostAddr))
	runenv.RecordMessage("created libp2p host")

	var bootstrap peer.AddrInfo

	switch runenv.TestGroupID {
	case "boot":
		myInfo := peer.AddrInfo{
			ID:    host.ID(),
			Addrs: host.Addrs(),
		}
		runenv.RecordMessage("publishing bootstrap info...")
		err := tgPublish(ctx, client, myInfo)
		if err != nil {
			panic(err)
		}
		bootstrap = myInfo
	case "worker":
		bootInfo, err := tgSubscribe(ctx, client, runenv)
		if err != nil {
			panic(err)
		}
		bootstrap = bootInfo
		runenv.RecordMessage("received bootstrap info...")
	default:
		runenv.RecordMessage("invalid group id")
	}

	runenv.RecordMessage("all peers ready, connecting...")
	err = host.Connect(ctx, bootstrap)
	if err != nil {
		panic(err)
	}
	runenv.RecordMessage("connected to boot node, discovering others...")
	go discoverPeers(ctx, host)

	gossipsub, _ := pubsub.NewGossipSub(ctx, host)
	topic, _ := gossipsub.Join(*topicNameFlag)
	runenv.RecordMessage("joined gossipsub topic")

	subscribe, err := topic.Subscribe()
	if err != nil {
		panic(err)
	}
	runenv.RecordMessage("subscribed gossipsub topic")
	printMessagesFrom(ctx, subscribe)

	switch seq {
	case 11:
		go streamFileTo(ctx, topic, runenv.StringParam("small"))
		runenv.RecordMessage("published small file")
	case 23:
		go streamFileTo(ctx, topic, runenv.StringParam("medium"))
		runenv.RecordMessage("published medium file")
	case 49:
		go streamFileTo(ctx, topic, runenv.StringParam("large"))
		runenv.RecordMessage("published large file")
	default:
	}

	return err
}

func discoverPeers(ctx context.Context, host host.Host) {
	kademliaDHT, err := dht.New(ctx, host)
	if err != nil {
		panic(err)
	}
	routingDiscovery := drouting.NewRoutingDiscovery(kademliaDHT)
	dutil.Advertise(ctx, routingDiscovery, *topicNameFlag)

	anyConnected := false
	for !anyConnected {
		peerChannel, err := routingDiscovery.FindPeers(ctx, *topicNameFlag)
		if err != nil {
			panic(err)
		}
		for peer := range peerChannel {
			if peer.ID == host.ID() {
				continue
			}
			err := host.Connect(ctx, peer)
			if err != nil {
				fmt.Printf("Failed connecting to %s, error: %s\n", peer.ID, err)
			} else {
				fmt.Println("Connected to:", peer.ID)
				anyConnected = true
			}
		}
	}
	fmt.Println("Peer discovery complete")
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
