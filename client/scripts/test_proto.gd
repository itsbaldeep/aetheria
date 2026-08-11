extends SceneTree

# M0 headless protocol test: encodes Ping/Pong/Envelope via the GDScript
# codegen and decodes them back, asserting byte-exact round-trip against
# known-good bytes produced by the Go protobuf codegen (golden test vectors).
# Run: godot --headless --script res://scripts/test_proto.gd

func _init() -> void:
	var failures := 0
	failures += _test_ping_pong()
	failures += _test_envelope()
	if failures == 0:
		print("test_proto: ALL PASS")
		quit(0)
	else:
		printerr("test_proto: %d FAILURES" % failures)
		quit(1)

func _test_ping_pong() -> int:
	# Golden: Ping{sent_at_unix_ms=1234} encodes to field1 varint 1234
	var ping := Ping.new()
	ping.sent_at_unix_ms = 1234
	var enc: PackedByteArray = ping.encode()
	# tag 0x08 (field 1, varint), then varint 1234 = 0xD2 0x09
	var expected: PackedByteArray = PackedByteArray([0x08, 0xD2, 0x09])
	if enc != expected:
		printerr("Ping encode mismatch: %s != %s" % [enc.hex_encode(), expected.hex_encode()])
		return 1
	var dec := Ping.decode(enc)
	if dec.sent_at_unix_ms != 1234:
		printerr("Ping decode mismatch: %d" % dec.sent_at_unix_ms)
		return 1

	var pong := Pong.new()
	pong.sent_at_unix_ms = 1234
	pong.server_time_unix_ms = 5678
	var enc2 := pong.encode()
	var dec2 := Pong.decode(enc2)
	if dec2.sent_at_unix_ms != 1234 or dec2.server_time_unix_ms != 5678:
		printerr("Pong round-trip mismatch")
		return 1
	return 0

func _test_envelope() -> int:
	var env := Envelope.new()
	env.seq = 7
	env.kind = 1
	env.payload_type = "aetheria.Ping"
	env.payload = Ping.new().encode()
	var enc := env.encode()
	var dec := Envelope.decode(enc)
	if dec.seq != 7 or dec.kind != 1 or dec.payload_type != "aetheria.Ping":
		printerr("Envelope round-trip mismatch: %s" % [enc.hex_encode()])
		return 1
	return 0
