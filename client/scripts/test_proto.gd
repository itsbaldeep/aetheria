extends SceneTree

# M0 headless protocol test: encodes Ping/Pong/Envelope via the GDScript
# codegen and decodes them back, asserting byte-exact round-trip against
# known-good bytes produced by the Go protobuf codegen (golden test vectors).
# Run: godot --headless --script res://scripts/test_proto.gd

func _init() -> void:
	var failures := 0
	failures += _test_ping_pong()
	failures += _test_envelope()
	failures += _test_vec3_float()
	if failures == 0:
		print("test_proto: ALL PASS")
		quit(0)
	else:
		printerr("test_proto: %d FAILURES" % failures)
		quit(1)

func _test_vec3_float() -> int:
	# Golden: Vec3{x=1.0,y=0,z=0} → field1 fixed32 0x0D + IEEE-754 LE 3F 80 00 00,
	# then field2/3 fixed32 tags 0x15/0x1D each with zero float bytes.
	var v := Vec3.new()
	v.x = 1.0
	var enc: PackedByteArray = v.encode()
	var expected: PackedByteArray = PackedByteArray([0x0D, 0x00, 0x00, 0x80, 0x3F, 0x15, 0x00, 0x00, 0x00, 0x00, 0x1D, 0x00, 0x00, 0x00, 0x00])
	if enc != expected:
		printerr("Vec3 float encode mismatch: %s != %s" % [enc.hex_encode(), expected.hex_encode()])
		return 1
	var dec := Vec3.decode(enc)
	if absf(dec.x - 1.0) > 1e-6:
		printerr("Vec3 float decode mismatch: %f" % dec.x)
		return 1
	# Round-trip of negative + fractional values.
	var v2 := Vec3.new()
	v2.x = -2.5
	v2.y = 12.125
	v2.z = 0.0
	var dec2 := Vec3.decode(v2.encode())
	if absf(dec2.x + 2.5) > 1e-6 or absf(dec2.y - 12.125) > 1e-6 or absf(dec2.z) > 1e-6:
		printerr("Vec3 float round-trip mismatch: %s" % [dec2.x, dec2.y, dec2.z])
		return 1
	return 0

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
