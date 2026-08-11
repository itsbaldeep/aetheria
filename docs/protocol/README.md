# Aetheria Wire Protocol — Reference

Single source of truth: `shared/proto/aetheria.proto`. This file is the
readable reference for the frames and the message flow. Rebuild generated
code with `make content` (Go + GDScript).

## Framing

Every WebSocket binary frame is one `aetheria.Envelope`:

| field | type | meaning |
|---|---|---|
| `seq` | uint64 | client-monotonic sequence for request/response pairing |
| `kind` | enum | 1 = request, 2 = event |
| `payload_type` | string | full message name, e.g. `aetheria.MoveIntent` |
| `payload` | bytes | protobuf-encoded message |

## Connection lifecycle (M0/M1)

1. Client connects to `wss://play.<domain>/ws` with HTTP header
   `Authorization: Bearer <session-token>`.
2. Server validates the token + ban status; rejects with a
   `StatusPolicyViolation` close (`invalid_session` / `account_banned`) before
   any message when invalid.
3. Server sends `ServerHello {protocol_version, game_name, tick_rate_hz}`.
4. Client may `Ping {sent_at_unix_ms}`; server replies `Pong` with the same
   timestamp + `server_time_unix_ms`.

## World presence (M2)

After ServerHello, to enter the world:

1. Client sends `EnterWorld {character_id}`.
2. Server loads the character (ownership-checked), spawns it, replies
   `EnterWorldAck {ok, error?, entity_id, zone_id, position, max_speed}`.
3. Server streams `WorldSnapshot` envelopes at 20 Hz:

```
WorldSnapshot {
  tick, self_id,
  self:        [EntityState]   // 0..1 authoritative echo of the player
  entities:    [EntityState]   // new/changed nearby entities (AOI deltas)
  despawn_ids: [uint64]        // entities that left the viewer's AOI
}
```

4. Client sends `MoveIntent {target?, direction?, speed, rot_y}` for
   movement. The server validates speed (clamped to `max_speed`), integrates
   on its 20 Hz tick, and echoes authoritative positions via `self`.

EntityState fields: `entity_id, entity_type, name, zone_id, position{x,y,z},
rot_y, speed, hp, max_hp, level, is_moving`.

5. Client sends `LeaveWorld` (or drops the socket) to exit; the server saves
   the position and despawns the player.

## Message index

- Envelope
- Ping, Pong
- ServerHello
- SessionStatus (reserved, M1)
- Vec3
- EnterWorld, EnterWorldAck
- MoveIntent
- EntityState, WorldSnapshot
- LeaveWorld
