// botclient — headless Go bot that speaks the real Aetheria protocol.
// This is the agent's eyes (brief §12.3): register→login→play via the same
// protobuf Envelope framing the Godot client uses.
//
// M0 profile: connect to the gameserver WS, expect ServerHello, send Ping,
// require Pong with matching sent_at, print result, exit.
//
// Usage:
//
//	go run ./tools/botclient -addr wss://play.aetheria.apps.deployden.tech/ws
//	go run ./tools/botclient -addr ws://127.0.0.1:3015/ws -profile ping
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/coder/websocket"
	"google.golang.org/protobuf/proto"

	aet "github.com/itsbaldeep/aetheria/server/gen"
)

func main() {
	addr := flag.String("addr", "wss://play.aetheria.apps.deployden.tech/ws", "gameserver websocket URL")
	profile := flag.String("profile", "ping", "behavior profile (ping|roamer|grinder|quester|trader|partygoer|chaos)")
	flag.Parse()

	switch *profile {
	case "ping":
		runPing(*addr)
	default:
		fmt.Fprintf(os.Stderr, "botclient: unknown profile %q (M0 implements 'ping' only)\n", *profile)
		os.Exit(2)
	}
}

func runPing(addr string) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, addr, nil)
	if err != nil {
		fatal("dial: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "done")

	// Expect ServerHello first.
	_, data, err := conn.Read(ctx)
	if err != nil {
		fatal("read hello: %v", err)
	}
	hello := &aet.ServerHello{}
	if err := proto.Unmarshal(data, hello); err != nil {
		fatal("unmarshal hello: %v", err)
	}
	fmt.Printf("hello: protocol=%s game=%s tick=%dhz\n", hello.ProtocolVersion, hello.GameName, hello.TickRateHz)

	// Send Ping.
	sentAt := time.Now().UnixMilli()
	ping := &aet.Envelope{
		Seq:         1,
		Kind:        aet.Envelope_KIND_REQUEST,
		PayloadType: "aetheria.Ping",
		Payload:     mustMarshal(&aet.Ping{SentAtUnixMs: sentAt}),
	}
	if err := conn.Write(ctx, websocket.MessageBinary, mustMarshal(ping)); err != nil {
		fatal("write ping: %v", err)
	}

	// Read Pong.
	_, data, err = conn.Read(ctx)
	if err != nil {
		fatal("read pong: %v", err)
	}
	pong := &aet.Pong{}
	if err := proto.Unmarshal(data, pong); err != nil {
		fatal("unmarshal pong: %v", err)
	}
	if pong.SentAtUnixMs != sentAt {
		fatal("pong echo mismatch: sent=%d got=%d", sentAt, pong.SentAtUnixMs)
	}
	fmt.Printf("ping/pong OK: roundtrip=%dms\n", time.Now().UnixMilli()-pong.SentAtUnixMs)
}

func mustMarshal(m proto.Message) []byte {
	b, err := proto.Marshal(m)
	if err != nil {
		fatal("marshal: %v", err)
	}
	return b
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "botclient: "+format+"\n", args...)
	os.Exit(1)
}
