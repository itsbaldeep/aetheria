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

## Scalars

| proto | wire | notes |
|---|---|---|
| `int32/int64/uint32/uint64/bool` | varint | bool = 0/1 |
| `float` | fixed32 (wire type 5) | 4 little-endian IEEE-754 bytes |
| `string` | length-delimited bytes | UTF-8 |
| `repeated T` | packed by field, `T` encoded per row | unchanged |

The GDScript `ProtoWire` runtime (tools/protogen) writes `float` as fixed32
to match the Go protobuf codegen; treat any length-delimited float in a
message payload as a decode error.

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

## Economy (M4)

Items, inventory, loot, and vendors (see BRIEF §212). Ground drops stream as
entities with `entity_type: "drop"`; the server enforces the pickup radius and
single-claim.

| client → server | server → client | notes |
|---|---|---|
| `PickupItem {drop_entity_id}` | `LootEvent` | claims a ground drop exactly once |
| `EquipItem {item_id}` | `LootEvent` | moves an inventory item to its slot |
| `UnequipItem {slot}` | `LootEvent` | moves an equipped item back to inventory |
| `SellItem {item_id, quantity}` | `LootEvent` | credits `vendor_price × qty` gold |
| `BuyItem {vendor_id, item_def_id, quantity}` | `LootEvent` | pays `vendor_price × qty` gold |

`LootEvent {ok, error?, item_id, item_def_id, quantity, gold, balance}`
confirms every mutation and reports the player's new gold balance. All gold
mutations write a signed `gold_ledger` row; the audit invariant is
`sum(gold_ledger) == world gold`.

## M5 additions

`EntityState` also carries self-only `mp`, `max_mp`, `xp` and `xp_for_level`
(fields 13–16) on the receiving player's own snapshot so the client can draw
mana and XP bars from live server data. Mobs/NPCs leave them zero.

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
- CastSkill, AutoAttack, CombatEvent, ChatMessage, RespawnRequest, RespawnAck (M3)
- PickupItem, EquipItem, UnequipItem, SellItem, BuyItem, LootEvent (M4)
