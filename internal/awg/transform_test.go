package awg

import (
	"encoding/binary"
	"testing"
)

func testConfig() *Config {
	return &Config{
		Jc:   3,
		Jmin: 30,
		Jmax: 500,
		S1:   20,
		S2:   20,
		H1:   1234567890,
		H2:   1234567891,
		H3:   1234567892,
		H4:   1234567893,
	}
}

func makePacket(msgType uint32, size int) []byte {
	buf := make([]byte, size)
	binary.LittleEndian.PutUint32(buf[:4], msgType)
	// Fill rest with deterministic data.
	for i := 4; i < size; i++ {
		buf[i] = byte(i)
	}
	return buf
}

func TestTransformOutboundHandshakeInit(t *testing.T) {
	cfg := testConfig()
	original := makePacket(wgHandshakeInit, WgHandshakeInitSize)
	payload := make([]byte, WgHandshakeInitSize)
	copy(payload, original)

	out, sendJunk := TransformOutbound(payload, WgHandshakeInitSize, cfg)

	if !sendJunk {
		t.Fatal("expected sendJunk=true for handshake init")
	}
	if len(out) != cfg.S1+WgHandshakeInitSize {
		t.Fatalf("expected len %d, got %d", cfg.S1+WgHandshakeInitSize, len(out))
	}
	// Check type at S1 offset.
	gotType := binary.LittleEndian.Uint32(out[cfg.S1 : cfg.S1+4])
	if gotType != cfg.H1 {
		t.Fatalf("expected type %d, got %d", cfg.H1, gotType)
	}
	// Check payload preserved after type.
	for i := 4; i < WgHandshakeInitSize; i++ {
		if out[cfg.S1+i] != original[i] {
			t.Fatalf("payload byte %d mismatch: expected %d, got %d", i, original[i], out[cfg.S1+i])
		}
	}
}

func TestTransformOutboundHandshakeResponse(t *testing.T) {
	cfg := testConfig()
	original := makePacket(wgHandshakeResponse, WgHandshakeResponseSize)
	payload := make([]byte, WgHandshakeResponseSize)
	copy(payload, original)

	out, sendJunk := TransformOutbound(payload, WgHandshakeResponseSize, cfg)

	if sendJunk {
		t.Fatal("expected sendJunk=false for handshake response")
	}
	if len(out) != cfg.S2+WgHandshakeResponseSize {
		t.Fatalf("expected len %d, got %d", cfg.S2+WgHandshakeResponseSize, len(out))
	}
	gotType := binary.LittleEndian.Uint32(out[cfg.S2 : cfg.S2+4])
	if gotType != cfg.H2 {
		t.Fatalf("expected type %d, got %d", cfg.H2, gotType)
	}
}

func TestTransformOutboundCookieReply(t *testing.T) {
	cfg := testConfig()
	payload := makePacket(wgCookieReply, WgCookieReplySize)

	out, sendJunk := TransformOutbound(payload, WgCookieReplySize, cfg)

	if sendJunk {
		t.Fatal("expected sendJunk=false for cookie reply")
	}
	if len(out) != WgCookieReplySize {
		t.Fatalf("expected len %d, got %d", WgCookieReplySize, len(out))
	}
	gotType := binary.LittleEndian.Uint32(out[:4])
	if gotType != cfg.H3 {
		t.Fatalf("expected type %d, got %d", cfg.H3, gotType)
	}
}

func TestTransformOutboundTransportData(t *testing.T) {
	cfg := testConfig()
	payload := makePacket(wgTransportData, 100)

	out, sendJunk := TransformOutbound(payload, 100, cfg)

	if sendJunk {
		t.Fatal("expected sendJunk=false for transport data")
	}
	if len(out) != 100 {
		t.Fatalf("expected len 100, got %d", len(out))
	}
	gotType := binary.LittleEndian.Uint32(out[:4])
	if gotType != cfg.H4 {
		t.Fatalf("expected type %d, got %d", cfg.H4, gotType)
	}
}

func TestTransformInboundHandshakeInit(t *testing.T) {
	cfg := testConfig()
	// Build an AWG handshake init: S1 random bytes + H1 type + payload.
	inner := makePacket(cfg.H1, WgHandshakeInitSize)
	buf := make([]byte, cfg.S1+WgHandshakeInitSize)
	copy(buf[cfg.S1:], inner)

	out, valid := TransformInbound(buf, len(buf), cfg)
	if !valid {
		t.Fatal("expected valid=true")
	}
	if len(out) != WgHandshakeInitSize {
		t.Fatalf("expected len %d, got %d", WgHandshakeInitSize, len(out))
	}
	gotType := binary.LittleEndian.Uint32(out[:4])
	if gotType != wgHandshakeInit {
		t.Fatalf("expected type %d, got %d", wgHandshakeInit, gotType)
	}
}

func TestTransformInboundHandshakeResponse(t *testing.T) {
	cfg := testConfig()
	inner := makePacket(cfg.H2, WgHandshakeResponseSize)
	buf := make([]byte, cfg.S2+WgHandshakeResponseSize)
	copy(buf[cfg.S2:], inner)

	out, valid := TransformInbound(buf, len(buf), cfg)
	if !valid {
		t.Fatal("expected valid=true")
	}
	if len(out) != WgHandshakeResponseSize {
		t.Fatalf("expected len %d, got %d", WgHandshakeResponseSize, len(out))
	}
	gotType := binary.LittleEndian.Uint32(out[:4])
	if gotType != wgHandshakeResponse {
		t.Fatalf("expected type %d, got %d", wgHandshakeResponse, gotType)
	}
}

func TestTransformInboundCookieReply(t *testing.T) {
	cfg := testConfig()
	buf := makePacket(cfg.H3, WgCookieReplySize)

	out, valid := TransformInbound(buf, len(buf), cfg)
	if !valid {
		t.Fatal("expected valid=true")
	}
	gotType := binary.LittleEndian.Uint32(out[:4])
	if gotType != wgCookieReply {
		t.Fatalf("expected type %d, got %d", wgCookieReply, gotType)
	}
}

func TestTransformInboundTransportData(t *testing.T) {
	cfg := testConfig()
	buf := makePacket(cfg.H4, 100)

	out, valid := TransformInbound(buf, len(buf), cfg)
	if !valid {
		t.Fatal("expected valid=true")
	}
	gotType := binary.LittleEndian.Uint32(out[:4])
	if gotType != wgTransportData {
		t.Fatalf("expected type %d, got %d", wgTransportData, gotType)
	}
}

func TestRoundtripHandshakeInit(t *testing.T) {
	cfg := testConfig()
	original := makePacket(wgHandshakeInit, WgHandshakeInitSize)
	saved := make([]byte, WgHandshakeInitSize)
	copy(saved, original)

	out, _ := TransformOutbound(original, WgHandshakeInitSize, cfg)
	result, valid := TransformInbound(out, len(out), cfg)
	if !valid {
		t.Fatal("roundtrip: inbound returned invalid")
	}
	if len(result) != WgHandshakeInitSize {
		t.Fatalf("roundtrip: expected len %d, got %d", WgHandshakeInitSize, len(result))
	}
	// Type should be restored.
	gotType := binary.LittleEndian.Uint32(result[:4])
	if gotType != wgHandshakeInit {
		t.Fatalf("roundtrip: expected type %d, got %d", wgHandshakeInit, gotType)
	}
	// Payload after type should be preserved.
	for i := 4; i < WgHandshakeInitSize; i++ {
		if result[i] != saved[i] {
			t.Fatalf("roundtrip: byte %d mismatch", i)
		}
	}
}

func TestRoundtripHandshakeResponse(t *testing.T) {
	cfg := testConfig()
	original := makePacket(wgHandshakeResponse, WgHandshakeResponseSize)
	saved := make([]byte, WgHandshakeResponseSize)
	copy(saved, original)

	out, _ := TransformOutbound(original, WgHandshakeResponseSize, cfg)
	result, valid := TransformInbound(out, len(out), cfg)
	if !valid {
		t.Fatal("roundtrip: inbound returned invalid")
	}
	gotType := binary.LittleEndian.Uint32(result[:4])
	if gotType != wgHandshakeResponse {
		t.Fatalf("roundtrip: expected type %d, got %d", wgHandshakeResponse, gotType)
	}
	for i := 4; i < WgHandshakeResponseSize; i++ {
		if result[i] != saved[i] {
			t.Fatalf("roundtrip: byte %d mismatch", i)
		}
	}
}

func TestRoundtripCookieReply(t *testing.T) {
	cfg := testConfig()
	original := makePacket(wgCookieReply, WgCookieReplySize)
	saved := make([]byte, WgCookieReplySize)
	copy(saved, original)

	out, _ := TransformOutbound(original, WgCookieReplySize, cfg)
	result, valid := TransformInbound(out, len(out), cfg)
	if !valid {
		t.Fatal("roundtrip: inbound returned invalid")
	}
	gotType := binary.LittleEndian.Uint32(result[:4])
	if gotType != wgCookieReply {
		t.Fatalf("roundtrip: expected type %d, got %d", wgCookieReply, gotType)
	}
}

func TestRoundtripTransportData(t *testing.T) {
	cfg := testConfig()
	original := makePacket(wgTransportData, 200)
	saved := make([]byte, 200)
	copy(saved, original)

	out, _ := TransformOutbound(original, 200, cfg)
	result, valid := TransformInbound(out, len(out), cfg)
	if !valid {
		t.Fatal("roundtrip: inbound returned invalid")
	}
	gotType := binary.LittleEndian.Uint32(result[:4])
	if gotType != wgTransportData {
		t.Fatalf("roundtrip: expected type %d, got %d", wgTransportData, gotType)
	}
	for i := 4; i < 200; i++ {
		if result[i] != saved[i] {
			t.Fatalf("roundtrip: byte %d mismatch", i)
		}
	}
}

func TestGenerateJunkPackets(t *testing.T) {
	cfg := testConfig()
	packets := GenerateJunkPackets(cfg)

	if len(packets) != cfg.Jc {
		t.Fatalf("expected %d junk packets, got %d", cfg.Jc, len(packets))
	}
	for i, pkt := range packets {
		if len(pkt) < cfg.Jmin || len(pkt) > cfg.Jmax {
			t.Fatalf("junk packet %d: size %d not in [%d, %d]", i, len(pkt), cfg.Jmin, cfg.Jmax)
		}
	}
}

func TestGenerateJunkPacketsZeroJc(t *testing.T) {
	cfg := testConfig()
	cfg.Jc = 0
	packets := GenerateJunkPackets(cfg)
	if packets != nil {
		t.Fatalf("expected nil for Jc=0, got %d packets", len(packets))
	}
}

func TestInboundDropsUnknown(t *testing.T) {
	cfg := testConfig()
	// Packet with unknown type.
	buf := makePacket(99999, 100)
	_, valid := TransformInbound(buf, len(buf), cfg)
	if valid {
		t.Fatal("expected valid=false for unknown packet type")
	}
}

func TestInboundDropsTooShort(t *testing.T) {
	cfg := testConfig()
	buf := []byte{1, 2, 3}
	_, valid := TransformInbound(buf, len(buf), cfg)
	if valid {
		t.Fatal("expected valid=false for too-short packet")
	}
}

func TestNoPaddingS1Zero(t *testing.T) {
	cfg := testConfig()
	cfg.S1 = 0
	original := makePacket(wgHandshakeInit, WgHandshakeInitSize)
	saved := make([]byte, WgHandshakeInitSize)
	copy(saved, original)

	out, sendJunk := TransformOutbound(original, WgHandshakeInitSize, cfg)
	if !sendJunk {
		t.Fatal("expected sendJunk=true even with S1=0")
	}
	if len(out) != WgHandshakeInitSize {
		t.Fatalf("expected len %d with S1=0, got %d", WgHandshakeInitSize, len(out))
	}

	// Roundtrip.
	result, valid := TransformInbound(out, len(out), cfg)
	if !valid {
		t.Fatal("roundtrip with S1=0: invalid")
	}
	gotType := binary.LittleEndian.Uint32(result[:4])
	if gotType != wgHandshakeInit {
		t.Fatalf("roundtrip with S1=0: expected type %d, got %d", wgHandshakeInit, gotType)
	}
}

func TestNoPaddingS2Zero(t *testing.T) {
	cfg := testConfig()
	cfg.S2 = 0
	original := makePacket(wgHandshakeResponse, WgHandshakeResponseSize)
	saved := make([]byte, WgHandshakeResponseSize)
	copy(saved, original)

	out, _ := TransformOutbound(original, WgHandshakeResponseSize, cfg)
	if len(out) != WgHandshakeResponseSize {
		t.Fatalf("expected len %d with S2=0, got %d", WgHandshakeResponseSize, len(out))
	}

	result, valid := TransformInbound(out, len(out), cfg)
	if !valid {
		t.Fatal("roundtrip with S2=0: invalid")
	}
	gotType := binary.LittleEndian.Uint32(result[:4])
	if gotType != wgHandshakeResponse {
		t.Fatalf("roundtrip with S2=0: expected type %d, got %d", wgHandshakeResponse, gotType)
	}
}

func TestOutboundTooShort(t *testing.T) {
	cfg := testConfig()
	buf := []byte{1, 2}
	out, sendJunk := TransformOutbound(buf, 2, cfg)
	if sendJunk {
		t.Fatal("expected sendJunk=false for too-short packet")
	}
	if len(out) != 2 {
		t.Fatalf("expected passthrough for too-short packet")
	}
}
