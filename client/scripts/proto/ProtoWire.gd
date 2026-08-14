# ProtoWire — minimal protobuf wire-format runtime for Aetheria's GDScript client.
# Supports the subset of proto3 used by shared/proto:
#   - varint fields: int32, int64, uint32, uint64, bool, enum
#   - length-delimited fields: string, bytes, nested message
# Generated classes call write_field(...)/read into the shared buffer.
# See ADR-003 for why this exists instead of godobuf.
class_name ProtoWire
extends RefCounted

const VARINT: int = 0
const I64: int = 1
const LEN: int = 2
const I32: int = 5

var buf: PackedByteArray = PackedByteArray()
var pos: int = 0

# --- encoding ---

func write_varint(v: int) -> void:
	if v >= 0:
		var u: int = v
		while true:
			var b: int = u & 0x7f
			u = u >> 7
			if u == 0:
				buf.append(b)
				break
			buf.append(b | 0x80)
	else:
		# negative int64 encodes as 10-byte sign-extended two's complement
		var u: int = v
		for i in range(9):
			buf.append((u & 0x7f) | 0x80)
			u = u >> 7
		buf.append(1)

func write_field(field_no: int, wire: int, v: int = 0, payload: PackedByteArray = PackedByteArray()) -> void:
	match wire:
		VARINT:
			write_varint(field_no << 3)
			write_varint(v)
		I64:
			write_varint(field_no << 3 | 1)
			var le: PackedByteArray = PackedByteArray([0, 0, 0, 0, 0, 0, 0, 0])
			var u: int = v
			for i in range(8):
				le[i] = u & 0xff
				u = u >> 8
			buf.append_array(le)
		LEN:
			write_varint(field_no << 3 | 2)
			write_varint(payload.size())
			buf.append_array(payload)
		I32:
			write_varint(field_no << 3 | 5)
			for i in range(4):
				buf.append(v & 0xff)
				v = v >> 8

func write_int64_field(field_no: int, v: int) -> void:
	write_field(field_no, VARINT, v)

func write_string_field(field_no: int, s: String) -> void:
	var bytes := s.to_utf8_buffer()
	write_field(field_no, LEN, 0, bytes)

func write_bytes_field(field_no: int, b: PackedByteArray) -> void:
	write_field(field_no, LEN, 0, b)

# proto3 float = fixed32 (wire type 5): 4 raw little-endian IEEE-754 bytes.
func write_float_field(field_no: int, v: float) -> void:
	write_varint(field_no << 3 | 5)
	var b := PackedByteArray()
	b.resize(4)
	b.encode_float(0, v)
	buf.append_array(b)

func read_float() -> float:
	var b := read_bytes(4)
	return b.decode_float(0)

# --- decoding ---

func read_varint() -> int:
	var result: int = 0
	var shift: int = 0
	while true:
		var b: int = buf[pos]
		pos += 1
		result |= (b & 0x7f) << shift
		if b & 0x80 == 0:
			break
		shift += 7
	return result

func read_bytes(n: int) -> PackedByteArray:
	var out := buf.slice(pos, pos + n)
	pos += n
	return out

func read_string() -> String:
	var n := read_varint()
	return read_bytes(n).get_string_from_utf8()

func skip(wire: int) -> void:
	match wire:
		VARINT:
			read_varint()
		I64:
			pos += 8
		LEN:
			read_bytes(read_varint())
		I32:
			pos += 4

# Returns [field_no, wire_type]. -1 field_no when exhausted.
func next_field() -> Array:
	if pos >= buf.size():
		return [-1, -1]
	var tag := read_varint()
	return [tag >> 3, tag & 0x7]
