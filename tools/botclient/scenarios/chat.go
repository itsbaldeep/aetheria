// Scenario: chat relay end-to-end (M3). Two bots enter the world far apart:
// A's `world` chat must reach B, A's `say` chat must not (out of range), and
// a muted player's message is rejected by the server (no relay to B).
package scenarios

import (
	"context"
	"fmt"
	"io"
	"time"
)

// ChatResult summarizes a chat relay run.
type ChatResult struct {
	WorldRelayed bool
	SayLeaked    bool
	MutedBlocked bool
}

// Chat runs the M3 chat relay scenario with two connected bots.
func Chat(wsURL, token string, charIDA, charIDB int64, timeout time.Duration, dbg io.Writer) (*ChatResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	a, err := ConnectWorld(ctx, wsURL, token, charIDA)
	if err != nil {
		return nil, fmt.Errorf("scenarios: connect A: %w", err)
	}
	defer a.Close()
	b, err := ConnectWorld(ctx, wsURL, token, charIDB)
	if err != nil {
		return nil, fmt.Errorf("scenarios: connect B: %w", err)
	}
	defer b.Close()

	// Both bots go quiet between chat sends (waiting on snapshot reads), which
	// would idle the server's inbound read loop in 10 s; a heartbeat keeps the
	// sockets fed.
	a.StartHeartbeat(ctx, 4*time.Second)
	b.StartHeartbeat(ctx, 4*time.Second)

	res := &ChatResult{}

	// Drain A and B's first snapshots (spawn).
	for _, bot := range []*WorldBot{a, b} {
		if !bot.ReadSnapshot(ctx) {
			return nil, fmt.Errorf("scenarios: no spawn snapshot from %d", bot.EntityID)
		}
	}

	// Move B far east so say (short range) must not reach: 200 m away.
	if err := b.Move(ctx, 1, 0, b.MaxSpeed); err != nil {
		return nil, fmt.Errorf("scenarios: move B: %w", err)
	}
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if ok, err := b.ReadSnapshotErr(ctx); !ok {
			return nil, fmt.Errorf("scenarios: B snapshot while moving: %v", err)
		}
		if b.PosX >= 190 {
			break
		}
	}
	if b.PosX < 190 {
		return nil, fmt.Errorf("scenarios: B never reached 190 m (x=%.0f)", b.PosX)
	}
	_ = b.Stop(ctx)

	// Move A out of the town pocket too, so both share the emberfield zone
	// (world chat is zone-scoped) while staying 100+ m apart (say is 30 m).
	if err := a.Move(ctx, 1, 0, a.MaxSpeed); err != nil {
		return nil, fmt.Errorf("scenarios: move A: %w", err)
	}
	deadline = time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if ok, err := a.ReadSnapshotErr(ctx); !ok {
			return nil, fmt.Errorf("scenarios: A snapshot while moving: %v", err)
		}
		if a.PosX >= 90 {
			break
		}
	}
	if a.PosX < 90 {
		return nil, fmt.Errorf("scenarios: A never reached 90 m (x=%.0f)", a.PosX)
	}
	_ = a.Stop(ctx)

	// World channel: A and B are in the same zone, so this must arrive.
	if err := a.SendChat(ctx, "world", "hello from A"); err != nil {
		return nil, fmt.Errorf("scenarios: send world chat: %w", err)
	}
	if err := waitForChat(ctx, b, "hello from A"); err != nil {
		return nil, fmt.Errorf("scenarios: world chat not relayed: %v", err)
	}
	res.WorldRelayed = true

	// Say channel: A and B are far apart (say is ~10 m), so B must NOT see it.
	if err := a.SendChat(ctx, "say", "nearby secret"); err != nil {
		return nil, fmt.Errorf("scenarios: send say chat: %w", err)
	}
	leaked, err := chatArrived(ctx, b, "nearby secret", 300*time.Millisecond)
	if err != nil {
		return nil, err
	}
	res.SayLeaked = leaked

	// Mute A via the sim (simulated GM action), then A's send is rejected and
	// nothing reaches B.
	// NOTE: no public GM/mute endpoint yet; covered by server unit test
	// (TestChatWorldAndMute). Here we just verify A can still chat when unmuted.
	if err := a.SendChat(ctx, "world", "second hello"); err != nil {
		return nil, fmt.Errorf("scenarios: resend world chat: %w", err)
	}
	if err := waitForChat(ctx, b, "second hello"); err != nil {
		return nil, fmt.Errorf("scenarios: second world chat not relayed: %v", err)
	}

	fmt.Fprintf(dbg, "chat: world relayed=%v say leaked=%v\n", res.WorldRelayed, res.SayLeaked)
	return res, nil
}

// waitForChat reads frames until a ChatMessage with the given text arrives.
func waitForChat(ctx context.Context, b *WorldBot, want string) error {
	start := len(b.Chat)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if env, err := b.ReadFrame(ctx); err != nil {
			return fmt.Errorf("read: %w", err)
		} else if env.PayloadType == "aetheria.WorldSnapshot" {
			// snapshots are fine; keep draining
		}
		for _, cm := range b.Chat[start:] {
			if cm.Text == want {
				return nil
			}
		}
	}
	return fmt.Errorf("timed out waiting for %q", want)
}

// chatArrived reports whether a chat with the given text arrived within d.
func chatArrived(ctx context.Context, b *WorldBot, want string, d time.Duration) (bool, error) {
	start := len(b.Chat)
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if _, err := b.ReadFrame(ctx); err != nil {
			return false, fmt.Errorf("read: %w", err)
		}
		for _, cm := range b.Chat[start:] {
			if cm.Text == want {
				return true, nil
			}
		}
	}
	return false, nil
}
