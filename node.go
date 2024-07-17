package main

import (
	"bufio"
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

	containerAddr, err := netclient.GetDataNetworkIP()
	if err != nil {
		panic(err)
	}

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
	netclient.MustConfigureNetwork(ctx, config)

	host, err := libp2p.New(libp2p.ListenAddrStrings("/ip4/%s/tcp/3000", containerAddr.String()))
	if err != nil {
		panic(err)
	}

	ready := sync.State("ready to bootstrap")
	tgBootstrap := sync.NewTopic("bootstrap", nodeInfo{})
	tgChannel := make(chan *nodeInfo, 1)

	if runenv.TestGroupID == "boot" {
		bootInfo := nodeInfo{
			ID:   host.ID().String(),
			addr: containerAddr.String(),
		}
		client.PublishAndWait(ctx, tgBootstrap, bootInfo, ready, runenv.TestInstanceCount-1)
	} else {
		client.Subscribe(ctx, tgBootstrap, tgChannel)
		client.SignalEntry(ctx, ready)
		bootInfo := <-tgChannel
		bootAddr, err := peer.AddrInfoFromString(fmt.Sprintf("/ip4/%s/tcp/3000", bootInfo.addr))
		if err != nil {
			panic(err)
		}
		host.Connect(ctx, *bootAddr)
	}

	go discoverPeers(ctx, host)

	gossipsub, err := pubsub.NewGossipSub(ctx, host)
	if err != nil {
		panic(err)
	}
	topic, err := gossipsub.Join(*topicNameFlag)
	if err != nil {
		panic(err)
	}
	go streamConsoleTo(ctx, topic)

	// TODO: change streamConsoleTo -> streamFileTo, with file dir specified in manifest.toml

	subscribe, err := topic.Subscribe()
	if err != nil {
		panic(err)
	}
	printMessagesFrom(ctx, subscribe)

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
				continue // No self connection
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

func streamConsoleTo(ctx context.Context, topic *pubsub.Topic) {
	reader := bufio.NewReader(os.Stdin)
	for {
		str, err := reader.ReadString('\n')
		if err != nil {
			panic(err)
		}
		if err := topic.Publish(ctx, []byte(str)); err != nil {
			fmt.Println("### Publish error:", err)
		}
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
