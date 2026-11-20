package core

import (
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"math"
	"math/bits"
)

type hongguoAccountProto []byte

func (p *hongguoAccountProto) number(field, value uint64) {
	*p = binary.AppendUvarint(*p, field<<3)
	*p = binary.AppendUvarint(*p, value)
}

func (p *hongguoAccountProto) bytes(field uint64, value []byte) {
	if len(value) == 0 {
		return
	}
	*p = binary.AppendUvarint(*p, field<<3|2)
	*p = binary.AppendUvarint(*p, uint64(len(value)))
	*p = append(*p, value...)
}

func (p *hongguoAccountProto) text(field uint64, value string) {
	p.bytes(field, []byte(value))
}

func (p *hongguoAccountProto) float(field uint64, value float32) {
	*p = binary.AppendUvarint(*p, field<<3|5)
	*p = binary.LittleEndian.AppendUint32(*p, math.Float32bits(value))
}

func hongguoAccountHex(value string) []byte {
	result, err := hex.DecodeString(value)
	if err != nil {
		panic(err)
	}
	return result
}

func hongguoAccountDigestSelector(query [32]byte, body [16]byte, seconds uint32) uint32 {
	value := uint32(0x20230928)
	for _, part := range [][]byte{query[:], body[:], binary.LittleEndian.AppendUint32(nil, seconds)} {
		for index, b := range part {
			if index%2 != 0 {
				value = ^(value ^ (value >> 7) ^ ((value << 12) | uint32(b)))
			} else {
				value = value ^ (value >> 4) ^ (value << 6) ^ uint32(b)
			}
		}
	}
	return value
}

func hongguoAccountDigest(query [32]byte, body [16]byte, seconds uint32) []byte {
	choice := hongguoAccountDigestSelector(query, body, seconds)
	if (choice&15)%3 != 2 {
		return nil
	}
	variant := ((choice & 13) * 86) >> 8
	bias := [...]uint32{0x8980f29b, 0xeb549c7f, 0xb08726db, 0xd40cb5e6, 0xe8f559e4}
	seed := seconds + bias[variant]
	message := append(append([]byte(nil), query[:]...), body[:]...)
	message = binary.LittleEndian.AppendUint32(message, seconds)
	for _, v := range hongguoAccountHex("8496779dd4150bf8") {
		message = append(message, bits.RotateLeft8(v, -int((seed+5)&7)))
	}
	message = binary.LittleEndian.AppendUint32(message, 416)
	var words [16]uint32
	for i := range words {
		words[i] = binary.LittleEndian.Uint32(message[i*4:])
	}
	iv := [4]uint32{0x79e0f2fb, 0xc8b52570, 0xebc2f8cd, 0x7c104d93}
	finish := [4]uint32{0x19be4866, 0xe85986b4, 0xe19b326e, 0x71d1d7d4}
	for i := range iv {
		iv[i] = bits.RotateLeft32(iv[i], -int(seed&31))
		finish[i] = bits.RotateLeft32(finish[i], -int((seed+6)&255))
	}
	a, b, c, d := iv[0], iv[1], iv[2], iv[3]
	shifts := [4][4]int{{7, 12, 17, 22}, {5, 9, 14, 20}, {4, 11, 16, 23}, {6, 10, 15, 21}}
	schedule := hongguoAccountDigestIndices[variant*64 : (variant+1)*64]
	for step := 0; step < 64; step++ {
		var f uint32
		switch step / 16 {
		case 0:
			f = (b & c) | (^b & d)
		case 1:
			f = (b & d) | (c & ^d)
		case 2:
			f = b ^ c ^ d
		default:
			f = c ^ (b | ^d)
		}
		constant := bits.RotateLeft32(hongguoAccountDigestConstants[step], -int((seed+1)&255))
		next := b + bits.RotateLeft32(a+f+words[schedule[step]]+constant, shifts[step/16][step%4])
		a, d, c, b = d, c, b, next
	}
	var result []byte
	for i, value := range [4]uint32{a, b, c, d} {
		result = binary.LittleEndian.AppendUint32(result, (value+iv[i])^finish[i])
	}
	checksum := uint32(0x20220420)
	for index, b := range result[:12] {
		if index%2 != 0 {
			checksum = checksum ^ (checksum >> 5) ^ ^((checksum << 11) | uint32(b))
		} else {
			checksum = checksum ^ (checksum >> 3) ^ (checksum << 7) ^ uint32(b)
		}
	}
	return binary.LittleEndian.AppendUint32(result, (checksum|4)^0x01000000)
}

func hongguoAccountDeviceProof(deviceID string) []byte {
	var p hongguoAccountProto
	p.number(1, 2)
	p.number(2, 4)
	p.text(3, "8662")
	p.text(4, deviceID)
	p.text(5, "Ai6svO3PyrwDOUSmO6ZcResxu")
	p.text(6, "!noperm!")
	p.number(7, 1777775)
	p.number(8, 1777775)
	p.number(9, 6)
	p.number(10, 1777775)
	p.text(11, "!notset!")
	p.text(12, "Asia/Shanghai,8")
	p.text(13, "zh_CN")
	p.number(14, 8)
	p.float(16, 255.24993896484375)
	p.float(17, 35.58599090576172)
	p.float(18, 3.467449188232422)
	p.float(19, 3.467449188232422)
	p.float(20, 255.1754913330078)
	p.float(21, 42.17544174194336)
	p.text(22, "16")
	p.number(23, 82)
	p.number(24, 72)
	for field := uint64(25); field <= 27; field++ {
		p.number(field, 3456776033270)
	}
	p.number(28, 3456776033274)
	p.number(29, 1)
	p.text(30, "25053RT47C")
	p.text(31, "Redmi")
	p.text(32, "25053RT47C")
	p.text(33, "25053RT47C")
	p.text(34, "Xiaomi")
	p.text(35, "Redmi")
	p.text(36, "Redmi")
	p.number(38, 62)
	return p
}

func hongguoAccountProof(query []byte, digest [32]byte, proof []byte, seconds uint32, deviceID, version string, nonce uint32) []byte {
	var sdk, clock, counters, p hongguoAccountProto
	clock.number(1, uint64(seconds)*2)
	clock.number(2, 3)
	clock.number(4, 400)
	sdk.number(1, 202)
	sdk.number(2, 292662798)
	sdk.number(3, 292662792)
	sdk.number(5, 14)
	sdk.text(6, "v04.06.04.03-bugfix")
	sdk.number(7, 20004)
	sdk.bytes(12, hongguoAccountDeviceProof(deviceID))
	sdk.bytes(13, clock)
	sdk.text(14, version)
	counters.number(1, 222)
	counters.number(2, 20)
	counters.number(3, 1388734)
	counters.number(5, 1173904398)
	p.bytes(1, hongguoAccountHex("f7e85ffad7d7dc3bd62ac87057cf6118"))
	p.number(2, 6)
	p.number(3, uint64(nonce)*2)
	p.text(4, "8662")
	p.text(5, deviceID)
	p.text(6, "1588093228")
	p.text(7, version)
	p.text(8, "v04.06.04-ml-android")
	p.number(9, 135006208)
	p.bytes(10, hongguoAccountHex("4001000000000000"))
	p.number(12, uint64(seconds)*2)
	p.bytes(13, proof)
	p.bytes(14, digest[:6])
	p.bytes(15, counters)
	p.text(16, "AXYQOS6n2m60x1fVZHIrH3iol")
	p.number(17, uint64(seconds)*2)
	inner := append(append([]byte(nil), query...), make([]byte, 16)...)
	inner = append(inner, "none"...)
	sum := hongguoSM3(inner)
	p.bytes(19, sum[:])
	p.text(20, "none")
	p.number(21, 624)
	p.bytes(23, sdk)
	p.text(24, `{"cmr":16777216,"cmr2":16777216,"un_h":1879194040,"vpn":0,"kd":0,"fkd":3672518972,"pd":-1872573247,"dyn":"","do":0,"tk":true}`)
	return p
}

func hongguoAccountScramble(payload []byte, key [32]byte) []byte {
	out := make([]byte, len(payload))
	for i, value := range payload {
		a, b := key[(i%8)*4], key[(i%8)*4+1]
		middle := bits.RotateLeft8((^(bits.RotateLeft8(value, 4) + a))^b, 3)
		out[len(payload)-1-i] = ^((middle + b) ^ a)
	}
	first := out[0]
	edge := out[len(out)-1] ^ out[len(out)-2]
	out[0] += ^edge
	out[1] += out[len(out)-1] ^ out[0] ^ 0xfe
	out[2] += (edge - first) ^ bits.RotateLeft8(out[1], 3) ^ 2
	for i := 3; i < len(out)-1; i++ {
		out[i] += ^(out[i-2] ^ bits.RotateLeft8(out[i-1], 3) ^ byte(i))
	}
	out[len(out)-1] ^= out[len(out)-2]
	out[0] ^= out[1]
	for _, value := range out[1:] {
		out[0] += value
	}
	return out
}

type hongguoAccountCipher struct {
	box   []byte
	rows  [4]int
	shift [16]int
	keys  [3][16]byte
}

func newHongguoAccountCipher(seconds uint32) hongguoAccountCipher {
	variant := seconds % 4
	table := hongguoAccountCipherTables[variant]
	box, _ := base64.StdEncoding.DecodeString(table.box)
	result := hongguoAccountCipher{box: box, rows: table.rows, shift: table.shift}
	constant := [4]uint32{0xca025ddc, 0x823dc546, 0xc9420583, 0xc298225f}[variant]
	key := hongguoAccountHex("f1593376766ea98d34f31b057a9d5be4")
	var words [12][4]byte
	for i := 0; i < 16; i++ {
		words[i/4][i%4] = key[i] ^ byte(constant>>uint((i%4)*8))
	}
	for i := 4; i < 12; i++ {
		word := words[i-1]
		if i%4 == 0 {
			word = [4]byte{box[word[1]], box[word[2]], box[word[3]], box[word[0]]}
			word[0] ^= byte(constant >> uint((2*i)&24))
		}
		for j := range word {
			words[i][j] = words[i-4][j] ^ word[j]
		}
	}
	for i, word := range words {
		copy(result.keys[i/4][(i%4)*4:], word[:])
	}
	return result
}

func (c hongguoAccountCipher) addKey(state [16]byte, key [16]byte) [16]byte {
	for row := 0; row < 4; row++ {
		for col := 0; col < 4; col++ {
			state[row*4+col] ^= key[row*4+c.rows[col]]
		}
	}
	return state
}

func (c hongguoAccountCipher) block(state [16]byte) [16]byte {
	state = c.addKey(state, c.keys[0])
	for step := 1; step <= 2; step++ {
		var reordered, mixed [16]byte
		for row := 0; row < 4; row++ {
			for col := 0; col < 4; col++ {
				reordered[row*4+col] = c.box[state[c.rows[row]*4+col]]
			}
		}
		for i, index := range c.shift {
			state[i] = reordered[index]
		}
		if step == 1 {
			for row := 0; row < 4; row++ {
				for col := 0; col < 4; col++ {
					mixed[row*4+col] = state[row*4+c.rows[col]]
				}
			}
			for col := 0; col < 4; col++ {
				total := mixed[col] ^ mixed[4+col] ^ mixed[8+col] ^ mixed[12+col]
				for row := 0; row < 4; row++ {
					v := mixed[row*4+col] ^ mixed[((row+1)%4)*4+col]
					twice := v << 1
					if v&128 != 0 {
						twice ^= 0x1b
					}
					state[row*4+col] = mixed[row*4+col] ^ total ^ twice
				}
			}
		}
		state = c.addKey(state, c.keys[step])
	}
	for i := range state {
		state[i] ^= c.keys[1][i]
	}
	return state
}

func (c hongguoAccountCipher) wrap(payload []byte) []byte {
	source := [8]uint{5, 6, 2, 4, 3, 1, 0, 7}
	packed := [8]uint{1, 6, 0, 7, 2, 4, 5, 3}
	var sample, encrypted [32]byte
	for group := 0; group < 31; group++ {
		for j := 0; j < 8; j++ {
			sample[group] |= ((payload[group*8+j] >> source[j]) & 1) << packed[j]
		}
	}
	sample[31] = 1
	var previous [16]byte
	copy(previous[:], hongguoAccountHex("1fe109a4125283f418de9e051a969e12"))
	for offset := 0; offset <= 16; offset += 16 {
		for i := range previous {
			previous[i] ^= sample[offset+i]
		}
		previous = c.block(previous)
		copy(encrypted[offset:], previous[:])
	}
	out := append([]byte(nil), payload...)
	for group := 0; group < 31; group++ {
		for j := 0; j < 8; j++ {
			out[group*8+j] = (out[group*8+j] & ^(byte(1) << source[j])) | (((encrypted[group] >> packed[j]) & 1) << source[j])
		}
	}
	return append([]byte{encrypted[31]}, out...)
}

func hongguoAccountMedusa(query []byte, seconds uint32, deviceID, version string, nonces [3]uint32) string {
	digest := hongguoSM3(query)
	proof := hongguoAccountDigest(digest, [16]byte{}, seconds)
	if proof == nil {
		return ""
	}
	message := hongguoAccountProof(query, digest, proof, seconds, deviceID, version, nonces[0])
	seed := hongguoAccountHex("8ebdfa3806ecc5cee79423e6029ed82540bc2218bb7eaef71cb691f7aa8aa2f5")
	derived := append([]byte(nil), seed...)
	derived = binary.LittleEndian.AppendUint32(derived, nonces[1])
	derived = append(derived, seed...)
	key := hongguoSM3(derived)
	n := nonces[1]
	b := (n >> 16) & 255
	mask := ^((((n >> 5) & 0x7f800) | (n >> 24)) ^ (b >> 5) ^ b)
	mixed := append(hongguoAccountHex("4001000000000000"), hongguoAccountScramble(message, key)...)
	for left, right := 0, len(mixed)-1; left < right; left, right = left+1, right-1 {
		mixed[left], mixed[right] = mixed[right], mixed[left]
	}
	for i := range mixed {
		mixed[i] ^= byte(mask >> uint(((^i)&3)*8))
	}
	flags := uint32(0x18000001) | (uint32(digest[0]&63) << 14) | (uint32(proof[0]&63) << 8)
	payload := binary.LittleEndian.AppendUint32([]byte{0x35}, nonces[2])
	payload = binary.LittleEndian.AppendUint32(payload, flags)
	payload = append(payload, mixed...)
	payload = binary.LittleEndian.AppendUint16(payload, uint16(n>>16))
	wrapped := newHongguoAccountCipher(seconds).wrap(payload)
	header := hongguoAccountHex("03000000f7e85ffad7d7dc3bd62ac87057cf6118")
	for i := range header {
		header[i] ^= byte(seconds >> uint((i%4)*8))
	}
	header = binary.LittleEndian.AppendUint16(header, uint16(n))
	header = append(header, 0, 1)
	return base64.StdEncoding.EncodeToString(append(header, wrapped...))
}
