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
	"github.com/testground/sdk-go/network"
	"github.com/testground/sdk-go/run"
	"github.com/testground/sdk-go/runtime"
	"github.com/testground/sdk-go/sync"
)

var (
	topicNameFlag = flag.String("topicName", "comnet", "name of topic to join")
)

type nodeInfo struct {
	ID   string
	addr string
}

func node(runenv *runtime.RunEnv, initCtx *run.InitContext) error {
	flag.Parse()
	ctx := context.Background()

	runenv.RecordMessage("before sync.MustBoundClient")
	client := initCtx.SyncClient
	netclient := initCtx.NetClient

	containerAddr, _ := netclient.GetDataNetworkIP()

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

	runenv.RecordMessage("before netclient.MustConfigureNetwork")
	ready := sync.State("network configured")

	netclient.MustConfigureNetwork(ctx, config)
	seq, _ := client.SignalEntry(ctx, ready)

	addr := fmt.Sprintf("/ip4/%s/tcp/3000", containerAddr.String())
	host, _ := libp2p.New(libp2p.ListenAddrStrings(addr))
	runenv.RecordMessage("created libp2p host")

	tgBootstrap := sync.NewTopic("bootstrap", nodeInfo{})
	tgChannel := make(chan *nodeInfo, 1)
	client.Subscribe(ctx, tgBootstrap, tgChannel)
	runenv.RecordMessage("subscribed to bootstrap info")

	if runenv.TestGroupID == "boot" {
		bootInfo := nodeInfo{
			ID:   host.ID().String(),
			addr: addr,
		}
		runenv.RecordMessage("before publishing bootstrap info")
		client.PublishSubscribe(ctx, tgBootstrap, bootInfo, tgChannel)
	}

	bootInfo := <-tgChannel
	bootAddr, _ := peer.AddrInfoFromString(bootInfo.addr)
	if bootAddr != nil {
		runenv.RecordMessage(fmt.Sprintf("received bootstrap info: %s, connecting...", bootInfo.addr))
	} else if bootInfo != nil {
		runenv.RecordMessage(fmt.Sprintf("failed to parse bootInfo: %s into multiaddr", bootInfo.addr))
	} else {
		runenv.RecordMessage("failed to receive bootInfo")
	}

	host.Connect(ctx, *bootAddr)
	runenv.RecordMessage("connected to boot node, discovering peers...")

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
		fmt.Println("Searching for peers...")
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
