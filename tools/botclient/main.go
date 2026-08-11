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
	"github.com/itsbaldeep/aetheria/tools/botclient/scenarios"
)

func main() {
	addr := flag.String("addr", "wss://play.aetheria.apps.deployden.tech/ws", "gameserver websocket URL")
	profile := flag.String("profile", "ping", "behavior profile (ping|register|login|create-char|roamer|grinder|quester|trader|partygoer|chaos)")
	api := flag.String("api", "http://127.0.0.1:3016", "authserver base URL (http://host:port)")
	n := flag.Int("n", 20, "count for batch profiles (register)")
	flag.Parse()

	switch *profile {
	case "ping":
		runPing(*addr)
	case "register":
		runRegister(*api, *n)
	case "login":
		runLogin(*api)
	case "create-char":
		runCreateChar(*api)
	default:
		fmt.Fprintf(os.Stderr, "botclient: unknown profile %q (M1 implements 'ping', 'register', 'login', 'create-char')\n", *profile)
		os.Exit(2)
	}
}

// runRegister drives N concurrent registrations (M1 acceptance).
func runRegister(apiURL string, n int) {
	// Timestamped email prefix so repeat runs never collide with leftover
	// accounts from a previous run (register is idempotent across runs).
	stamp := time.Now().UTC().Format("20060102T150405")
	cfg := scenarios.RegisterConfig{
		BaseURL:   apiURL,
		Count:     n,
		EmailFmt:  "botreg-" + stamp + "-%d@aetheria.test",
		Password:  "bot-pass-42",
		BatchSize: 8,
	}
	ok, err := scenarios.RegisterBatch(cfg)
	if err != nil {
		fatal("register batch: %v", err)
	}
	fmt.Printf("register OK: %d/%d accounts created\n", ok, n)
	if ok != n {
		fatal("register: only %d/%d succeeded", ok, n)
	}
}

// runLogin exercises the M1-2 login acceptance: a correct credential pair
// yields a token, wrong password / unknown email / banned account are
// rejected. Registers a throwaway account first so the email is real.
func runLogin(apiURL string) {
	stamp := time.Now().UTC().Format("20060102T150405")
	email := "botlogin-" + stamp + "@aetheria.test"
	pw := "login-pass-77"

	regCfg := scenarios.RegisterConfig{
		BaseURL: apiURL, Count: 1, EmailFmt: email, Password: pw, BatchSize: 1,
	}
	if _, err := scenarios.RegisterBatch(regCfg); err != nil {
		fatal("seed account: %v", err)
	}

	// Correct credentials → token.
	ok, err := scenarios.Login(apiURL, email, pw)
	if err != nil {
		fatal("login: %v", err)
	}
	if ok.Status != 200 {
		fatal("login(correct) status=%d want 200 (err=%s)", ok.Status, ok.Error)
	}
	if ok.Token == "" {
		fatal("login(correct) returned no token")
	}
	if ok.AccountID == 0 {
		fatal("login(correct) returned no account id")
	}
	fmt.Printf("login OK: token issued for account %d, expires %s\n", ok.AccountID, ok.ExpiresAt)

	// Wrong password → 401.
	bad, err := scenarios.Login(apiURL, email, "wrong-password-99")
	if err != nil {
		fatal("login wrong pw: %v", err)
	}
	if bad.Status != 401 {
		fatal("login(wrong pw) status=%d want 401", bad.Status)
	}
	fmt.Printf("login OK: wrong password rejected (401)\n")

	// Unknown email → 401.
	unk, err := scenarios.Login(apiURL, "nobody@aetheria.test", pw)
	if err != nil {
		fatal("login unknown: %v", err)
	}
	if unk.Status != 401 {
		fatal("login(unknown) status=%d want 401", unk.Status)
	}
	fmt.Printf("login OK: unknown email rejected (401)\n")

	// Banned account → 403. The banned email is supplied out-of-band (a
	// previous run or admin action set banned_until); skip if unset.
	if banned := os.Getenv("AETHERIA_BANNED_TEST_EMAIL"); banned != "" {
		b, err := scenarios.Login(apiURL, banned, pw)
		if err != nil {
			fatal("login banned: %v", err)
		}
		if b.Status != 403 {
			fatal("login(banned) status=%d want 403 (err=%s)", b.Status, b.Error)
		}
		fmt.Printf("login OK: banned account rejected (403)\n")
	}
}

// runCreateChar exercises M1-3: register → login → create character →
// list roster; also asserts server-side name/class validation and
// authentication gating.
func runCreateChar(apiURL string) {
	stamp := time.Now().UTC().Format("20060102T150405")
	email := "botchar-" + stamp + "@aetheria.test"
	pw := "charbot-pass-7"

	if _, err := scenarios.RegisterBatch(scenarios.RegisterConfig{
		BaseURL: apiURL, Count: 1, EmailFmt: email, Password: pw, BatchSize: 1,
	}); err != nil {
		fatal("seed account: %v", err)
	}
	lg, err := scenarios.Login(apiURL, email, pw)
	if err != nil {
		fatal("login: %v", err)
	}
	if lg.Token == "" {
		fatal("login returned no token")
	}

	// No token → 401.
	if st, _, _ := scenarios.CreateCharacter(apiURL, "", "NoAuth", ClassBladeDancer); st != 401 {
		fatal("create-char(no token) status=%d want 401", st)
	}
	fmt.Printf("create-char OK: unauthenticated create rejected (401)\n")

	// Bad name → 400.
	if st, _, _ := scenarios.CreateCharacter(apiURL, lg.Token, "a", ClassBladeDancer); st != 400 {
		fatal("create-char(bad name) status=%d want 400", st)
	}
	// Bad class → 400.
	if st, _, _ := scenarios.CreateCharacter(apiURL, lg.Token, "ValidName", "knight"); st != 400 {
		fatal("create-char(bad class) status=%d want 400", st)
	}
	fmt.Printf("create-char OK: bad name/class rejected (400)\n")

	// Valid create → 201.
	name := "Aria" + fmt.Sprint(stamp[len(stamp)-4:])
	st, _, err := scenarios.CreateCharacter(apiURL, lg.Token, name, ClassBladeDancer)
	if err != nil {
		fatal("create-char: %v", err)
	}
	if st != 201 {
		fatal("create-char(valid) status=%d want 201", st)
	}
	fmt.Printf("create-char OK: created %q (blade_dancer)\n", name)

	// Duplicate name → 409.
	if st, _, _ := scenarios.CreateCharacter(apiURL, lg.Token, name, ClassSpellweaver); st != 409 {
		fatal("create-char(dup name) status=%d want 409", st)
	}
	fmt.Printf("create-char OK: duplicate name rejected (409)\n")

	// Roster lists the character.
	chars, st, err := scenarios.ListCharacters(apiURL, lg.Token)
	if err != nil {
		fatal("list chars: %v", err)
	}
	if st != 200 {
		fatal("list chars status=%d want 200", st)
	}
	if len(chars) != 1 {
		fatal("list chars len=%d want 1", len(chars))
	}
	if chars[0]["name"] != name || chars[0]["class"] != ClassBladeDancer {
		fatal("list chars returned wrong character: %v", chars[0])
	}
	fmt.Printf("create-char OK: roster lists %q (class=%s)\n", chars[0]["name"], chars[0]["class"])
}

const ClassBladeDancer = "blade_dancer"
const ClassSpellweaver = "spellweaver"

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
