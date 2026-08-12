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
	"net/http"
	"os"
	"time"

	"github.com/coder/websocket"
	"google.golang.org/protobuf/proto"

	aet "github.com/itsbaldeep/aetheria/server/gen"
	"github.com/itsbaldeep/aetheria/tools/botclient/scenarios"
)

func main() {
	addr := flag.String("addr", "wss://play.aetheria.apps.deployden.tech/ws", "gameserver websocket URL")
	profile := flag.String("profile", "ping", "behavior profile (ping|register|login|create-char|full-auth|presence|roamer|combat|chaos)")
	api := flag.String("api", "http://127.0.0.1:3016", "authserver base URL (http://host:port)")
	n := flag.Int("n", 20, "count for batch profiles (register/roamer/chaos)")
	duration := flag.Duration("duration", 10*time.Second, "duration for soak profiles (roamer/chaos)")
	flag.Parse()

	switch *profile {
	case "ping":
		runPing(*addr, *api)
	case "register":
		runRegister(*api, *n)
	case "login":
		runLogin(*api)
	case "create-char":
		runCreateChar(*api)
	case "full-auth":
		runFullAuth(*addr, *api)
	case "presence":
		runPresence(*addr, *api)
	case "roamer":
		runRoamer(*addr, *api, *n, *duration)
	case "combat":
		runCombat(*addr, *api)
	case "chaos":
		runChaos(*addr, *api, *n, *duration)
	default:
		fmt.Fprintf(os.Stderr, "botclient: unknown profile %q (profiles: ping register login create-char full-auth presence roamer combat chaos)\n", *profile)
		os.Exit(2)
	}
}

// runPresence is the M2 acceptance: two clients see each other's movement
// with correct AOI (spawn → move → despawn).
func runPresence(wsURL, apiURL string) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	stamp := time.Now().UTC().Format("20060102T150405")
	var tokens []string
	var chars []int64
	var names []string
	for i := 1; i <= 2; i++ {
		email := fmt.Sprintf("botpresence-%s-%d@aetheria.test", stamp, i)
		if _, err := scenarios.RegisterBatch(scenarios.RegisterConfig{
			BaseURL: apiURL, Count: 1, EmailFmt: email, Password: "pres-pass-21", BatchSize: 1,
		}); err != nil {
			fatal("seed %d: %v", i, err)
		}
		lg, err := scenarios.Login(apiURL, email, "pres-pass-21")
		if err != nil || lg.Token == "" {
			fatal("login %d: %v", i, err)
		}
		tokens = append(tokens, lg.Token)
		name := fmt.Sprintf("Pres%d%s", i, stamp[len(stamp)-2:])
		// Use a per-index unique name (both under different accounts so name
		// conflicts impossible across runs thanks to the timestamp).
		if st, body, _ := scenarios.CreateCharacter(apiURL, lg.Token, name, ClassBladeDancer); st != 201 {
			fatal("create char %d: status=%d body=%s", i, st, body)
		}
		roster, st, err := scenarios.ListCharacters(apiURL, lg.Token)
		if err != nil || st != 200 {
			fatal("roster %d: %v", i, err)
		}
		if len(roster) != 1 {
			fatal("roster %d len=%d", i, len(roster))
		}
		chars = append(chars, int64(roster[0]["id"].(float64)))
		names = append(names, name)
	}

	res, err := scenarios.Presence(wsURL, tokens, chars, names, 60*time.Second)
	if err != nil {
		fatal("presence: %v", err)
	}
	fmt.Printf("presence OK: A saw B spawn=%v move=%v despawn=%v | B saw A spawn=%v move=%v despawn=%v\n",
		res.ASawBSpawn, res.ASawBMove, res.ASawBDespawn,
		res.BSawASpawn, res.BSawAMove, res.BSawADespawn)
	if !res.ASawBSpawn || !res.BSawASpawn {
		fatal("presence: mutual spawn missing")
	}
	if !res.ASawBMove || !res.BSawAMove {
		fatal("presence: mutual movement missing")
	}
	if !res.ASawBDespawn || !res.BSawADespawn {
		fatal("presence: mutual despawn missing")
	}
	fmt.Println("presence: ALL PASS — M2 acceptance met")
	_ = ctx
}

// runRoamer connects N bots, enters the world, and roams random directions
// for `duration`, exercising the 20 Hz snapshot stream under sustained load.
// This is the loadtest/soak profile (brief §12.3 `roamer`).
func runRoamer(wsURL, apiURL string, botCount int, duration time.Duration) {
	if botCount <= 0 {
		fatal("roamer: -n must be > 0")
	}
	ctx, cancel := context.WithTimeout(context.Background(), duration+120*time.Second)
	defer cancel()

	stamp := time.Now().UTC().Format("20060102T150405")
	type botSet struct {
		bot   *scenarios.WorldBot
		email string
	}
	var bots []*botSet
	for i := 1; i <= botCount; i++ {
		email := fmt.Sprintf("botroam-%s-%d@aetheria.test", stamp, i)
		if _, err := scenarios.RegisterBatch(scenarios.RegisterConfig{
			BaseURL: apiURL, Count: 1, EmailFmt: email, Password: "roam-pass-12", BatchSize: 1,
		}); err != nil {
			fatal("seed %d: %v", i, err)
		}
		lg, err := scenarios.Login(apiURL, email, "roam-pass-12")
		if err != nil || lg.Token == "" {
			fatal("login %d: %v", i, err)
		}
		name := fmt.Sprintf("Roam%s%d", stamp[len(stamp)-4:], i)
		if st, body, _ := scenarios.CreateCharacter(apiURL, lg.Token, name, ClassBladeDancer); st != 201 {
			fatal("create char %d: status=%d body=%s", i, st, body)
		}
		roster, st, err := scenarios.ListCharacters(apiURL, lg.Token)
		if err != nil || st != 200 || len(roster) != 1 {
			fatal("roster %d: len=%d st=%d err=%v", i, len(roster), st, err)
		}
		charID := int64(roster[0]["id"].(float64))
		bot, err := scenarios.ConnectWorld(ctx, wsURL, lg.Token, charID)
		if err != nil {
			fatal("connect %d: %v", i, err)
		}
		bots = append(bots, &botSet{bot: bot, email: email})
		defer bot.Close()
	}

	// Roam: each tick-pick a random direction per bot, send intents, and
	// consume snapshots. Track total snapshots seen + speed-hack attempts.
	totalSnaps := 0
	start := time.Now()
	dirIdx := make([]int, botCount)
	for time.Since(start) < duration {
		for i, bs := range bots {
			// Flip direction roughly every second.
			if int(time.Since(start).Seconds())%2 != dirIdx[i] {
				continue
			}
			dirs := [][2]float64{{1, 0}, {-1, 0}, {0, 1}, {0, -1}, {1, 1}, {-1, 1}}
			d := dirs[i%len(dirs)]
			if err := bs.bot.Move(ctx, d[0], d[1], bs.bot.MaxSpeed); err != nil {
				fatal("roam move %d: %v", i, err)
			}
			dirIdx[i] = int(time.Since(start).Seconds()) % 2
		}
		// Consume one snapshot per bot per ~50ms.
		for _, bs := range bots {
			if bs.bot.ReadSnapshot(ctx) {
				totalSnaps++
			}
		}
	}
	elapsed := time.Since(start)
	fmt.Printf("roamer OK: %d bots roamed for %s, snapshots consumed=%d (%.0f/s)\n",
		botCount, elapsed.Round(time.Millisecond), totalSnaps, float64(totalSnaps)/elapsed.Seconds())
	for i, bs := range bots {
		if bs.bot.SnapshotCount == 0 {
			fatal("roamer: bot %d saw zero snapshots", i+1)
		}
	}
	fmt.Println("roamer: ALL PASS — every bot streamed snapshots")
}

// runCombat is the M3 acceptance: one bot kills a boar (gains XP), then dies
// to Ashmaw and respawns at the shrine.
func runCombat(wsURL, apiURL string) {
	stamp := time.Now().UTC().Format("20060102T150405")
	email := fmt.Sprintf("botcombat-%s@aetheria.test", stamp)
	seed := scenarios.RegisterConfig{BaseURL: apiURL, Count: 1, EmailFmt: email, Password: "combat-pass-5", BatchSize: 1}
	if _, err := scenarios.RegisterBatch(seed); err != nil {
		fatal("seed: %v", err)
	}
	lg, err := scenarios.Login(apiURL, email, "combat-pass-5")
	if err != nil || lg.Token == "" {
		fatal("login: %v", err)
	}
	name := "Combat" + stamp[len(stamp)-3:]
	if st, _, _ := scenarios.CreateCharacter(apiURL, lg.Token, name, ClassBladeDancer); st != 201 {
		fatal("create char: status=%d", st)
	}
	roster, st, _ := scenarios.ListCharacters(apiURL, lg.Token)
	if st != 200 || len(roster) != 1 {
		fatal("roster len=%d st=%d", len(roster), st)
	}
	charID := int64(roster[0]["id"].(float64))

	res, err := scenarios.Combat(wsURL, lg.Token, charID, 180*time.Second, os.Stderr)
	if err != nil {
		fatal("combat: %v", err)
	}
	if res.SnapshotCount == 0 {
		fatal("combat: no snapshots seen")
	}
	if !res.KilledBoar {
		fatal("combat: boar never killed")
	}
	if res.XPgained <= 0 {
		fatal("combat: no XP gained")
	}
	if !res.Died {
		fatal("combat: bot never died to Ashmaw")
	}
	if !res.Respawned {
		fatal("combat: bot never respawned")
	}
	if res.HPAfterRespawn <= 0 {
		fatal("combat: respawned with HP=%d", res.HPAfterRespawn)
	}
	fmt.Printf("combat ALL PASS: boar killed (+%d XP), died, respawned HP=%d\n", res.XPgained, res.HPAfterRespawn)
}

// runChaos is the mandatory fuzzer (brief §12.3): sends random valid+invalid
// payloads and malformed bytes at the server and asserts it never crashes or
// wedges. The server may (correctly) close a socket that sends unparsable
// raw bytes; the assertion is that a fresh connection always works and a
// ping round-trip succeeds.
func runChaos(wsURL, apiURL string, n int, duration time.Duration) {
	ctx, cancel := context.WithTimeout(context.Background(), duration+120*time.Second)
	defer cancel()

	stamp := time.Now().UTC().Format("20060102T150405")
	email := fmt.Sprintf("botchaos-%s@aetheria.test", stamp)
	if _, err := scenarios.RegisterBatch(scenarios.RegisterConfig{
		BaseURL: apiURL, Count: 1, EmailFmt: email, Password: "chaos-pass-9", BatchSize: 1,
	}); err != nil {
		fatal("seed: %v", err)
	}
	lg, err := scenarios.Login(apiURL, email, "chaos-pass-9")
	if err != nil || lg.Token == "" {
		fatal("login: %v", err)
	}
	if st, _, _ := scenarios.CreateCharacter(apiURL, lg.Token, "Chaos"+stamp[len(stamp)-4:], ClassSpellweaver); st != 201 && st != 409 {
		fatal("create char: %d", st)
	}
	roster, _, _ := scenarios.ListCharacters(apiURL, lg.Token)
	if len(roster) != 1 {
		fatal("chaos roster len=%d", len(roster))
	}
	charID := int64(roster[0]["id"].(float64))

	rng := newChaosRNG()
	start := time.Now()
	sent := 0
	closed := 0
	for time.Since(start) < duration {
		// Reconnect if the previous socket was closed (proves the server is
		// still alive and accepting).
		bot, err := scenarios.ConnectWorld(ctx, wsURL, lg.Token, charID)
		if err != nil {
			// A connect failure after a fuzz storm = server likely wedged.
			if _, pingErr := scenarios.ConnectWorld(ctx, wsURL, lg.Token, charID); pingErr != nil {
				fatal("chaos: server unresponsive after fuzzing: %v", pingErr)
			}
			continue
		}
		payload := rng.randomBytes(64)
		var frame []byte
		if rng.bool() {
			env := &aet.Envelope{
				Seq:         rng.uint64(),
				Kind:        aet.Envelope_Kind(rng.intn(4)),
				PayloadType: rng.payloadType(),
				Payload:     payload,
			}
			frame = mustMarshal(env)
		} else {
			frame = payload // unparsable garbage
		}
		if err := bot.RawWrite(ctx, frame); err != nil {
			closed++
			bot.Close()
			continue
		}
		bot.ReadSnapshot(ctx)
		sent++
		bot.Close()
		_ = closed
	}
	// Final liveness: a fresh connection must accept, enter the world, and
	// ping/pong.
	final, err := scenarios.ConnectWorld(ctx, wsURL, lg.Token, charID)
	if err != nil {
		fatal("chaos: server dead after fuzzing: %v", err)
	}
	defer final.Close()
	if _, err := final.PingRoundTrip(ctx); err != nil {
		fatal("chaos: ping failed after fuzzing: %v", err)
	}
	fmt.Printf("chaos OK: %d frames sent over %s (%d sockets closed by server), fresh connection healthy\n",
		sent, time.Since(start).Round(time.Millisecond), closed)
	fmt.Println("chaos: ALL PASS — malformed input did not crash the server")
}

// runFullAuth is the M1 acceptance scenario (brief §11 M1): register → login
// → create character → authenticated WS session; wrong password / banned /
// invalid token all rejected.
func runFullAuth(wsURL, apiURL string) {
	stamp := time.Now().UTC().Format("20060102T150405")
	email := "botauth-" + stamp + "@aetheria.test"
	pw := "auth-pass-31"

	// 1. Register.
	if _, err := scenarios.RegisterBatch(scenarios.RegisterConfig{
		BaseURL: apiURL, Count: 1, EmailFmt: email, Password: pw, BatchSize: 1,
	}); err != nil {
		fatal("register: %v", err)
	}
	fmt.Printf("full-auth OK: registered %s\n", email)

	// 2. Login → token.
	lg, err := scenarios.Login(apiURL, email, pw)
	if err != nil {
		fatal("login: %v", err)
	}
	if lg.Token == "" {
		fatal("login: no token returned")
	}
	fmt.Printf("full-auth OK: logged in, token issued\n")

	// 3. Create character.
	name := "AuthHero" + stamp[len(stamp)-4:]
	if st, _, _ := scenarios.CreateCharacter(apiURL, lg.Token, name, ClassBladeDancer); st != 201 {
		fatal("create char: status=%d want 201", st)
	}
	fmt.Printf("full-auth OK: created character %s\n", name)

	// 4. Authenticated WS session → ServerHello.
	if p, err := scenarios.ConnectAuthed(wsURL, lg.Token, true); err != nil {
		fatal("authed ws: %v", err)
	} else {
		fmt.Printf("full-auth OK: authenticated WS session (hello accepted)\n")
		_ = p
	}

	// 5. No token → rejected.
	if _, err := scenarios.ConnectAuthed(wsURL, "", false); err != nil {
		fatal("no-token ws: %v", err)
	}
	fmt.Printf("full-auth OK: no-token connection rejected\n")

	// 6. Garbage token → rejected.
	if _, err := scenarios.ConnectAuthed(wsURL, "not-a-real-token", false); err != nil {
		fatal("bad-token ws: %v", err)
	}
	fmt.Printf("full-auth OK: invalid-token connection rejected\n")

	// 7. Wrong password → 401 (already covered in login, assert here too).
	if bad, err := scenarios.Login(apiURL, email, "wrong-password"); err != nil {
		fatal("wrong pw: %v", err)
	} else if bad.Status != 401 {
		fatal("wrong pw status=%d want 401", bad.Status)
	}
	fmt.Printf("full-auth OK: wrong password rejected (401)\n")

	// 8. Banned account rejected at WS handshake. Requires a token for an
	// account banned AFTER login (AETHERIA_BANNED_TEST_TOKEN, set by the
	// caller/CI). Skipped when unset; the login-time ban (403) is already
	// covered by the `login` profile.
	if bannedTok := os.Getenv("AETHERIA_BANNED_TEST_TOKEN"); bannedTok != "" {
		if p, err := scenarios.ConnectAuthed(wsURL, bannedTok, false); err != nil {
			fatal("banned ws: %v", err)
		} else if p.CloseReason != "account_banned" {
			fatal("banned ws close reason = %q, want account_banned", p.CloseReason)
		}
		fmt.Printf("full-auth OK: banned account rejected at handshake\n")
	}

	fmt.Println("full-auth: ALL PASS — M1 acceptance met")
}

// runPing authenticates (register a throwaway account, log in for a token),
// then connects to the gameserver WS with the Bearer token and completes the
// M0 ping/pong over an authenticated session (M1 handshake requirement).
func runPing(addr, apiURL string) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	stamp := time.Now().UTC().Format("20060102T150405")
	email := "botping-" + stamp + "@aetheria.test"
	pw := "ping-pass-42"
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
	fmt.Printf("auth OK: token for account %d\n", lg.AccountID)

	conn, _, err := websocket.Dial(ctx, addr, &websocket.DialOptions{
		HTTPHeader: http.Header{"Authorization": {"Bearer " + lg.Token}},
	})
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
	fmt.Printf("ping/pong OK (authed): roundtrip=%dms\n", time.Now().UnixMilli()-pong.SentAtUnixMs)
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

// chaosRNG is a small deterministic-ish fuzz source (crypto/rand seeded).
type chaosRNG struct {
	seed uint64
}

func newChaosRNG() *chaosRNG { return &chaosRNG{seed: uint64(time.Now().UnixNano())} }

func (c *chaosRNG) next() uint64 {
	// xorshift64
	c.seed ^= c.seed << 13
	c.seed ^= c.seed >> 7
	c.seed ^= c.seed << 17
	return c.seed
}

func (c *chaosRNG) bool() bool { return c.next()&1 == 1 }
func (c *chaosRNG) intn(n int) int {
	if n <= 0 {
		return 0
	}
	return int(c.next() % uint64(n))
}
func (c *chaosRNG) uint64() uint64 { return c.next() }

func (c *chaosRNG) randomBytes(n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(c.next() >> (8 * (i % 8)))
	}
	return b
}

func (c *chaosRNG) payloadType() string {
	types := []string{
		"aetheria.Ping", "aetheria.MoveIntent", "aetheria.EnterWorld",
		"aetheria.LeaveWorld", "aetheria.Nope", "", "aetheria.",
		"aetheria.WorldSnapshot", "random.string", "aetheria.Pong",
	}
	return types[c.intn(len(types))]
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
