package awg

import (
	"encoding/binary"
	"math/rand/v2"
)

// randFill fills b with pseudo-random bytes using math/rand/v2.
func randFill(b []byte) {
	for i := 0; i+8 <= len(b); i += 8 {
		binary.LittleEndian.PutUint64(b[i:i+8], rand.Uint64())
	}
	// Handle remaining bytes.
	tail := len(b) & 7
	if tail > 0 {
		v := rand.Uint64()
		off := len(b) - tail
		for j := 0; j < tail; j++ {
			b[off+j] = byte(v >> (j * 8))
		}
	}
}

// Standard WireGuard message types (little-endian uint32 in first 4 bytes).
const (
	wgHandshakeInit     uint32 = 1
	wgHandshakeResponse uint32 = 2
	wgCookieReply       uint32 = 3
	wgTransportData     uint32 = 4
)

// Standard WireGuard packet sizes.
const (
	WgHandshakeInitSize     = 148
	WgHandshakeResponseSize = 92
	WgCookieReplySize       = 64
	WgTransportMinSize      = 32
)

// Config holds AmneziaWG obfuscation parameters.
type Config struct {
	Jc   int    // junk packet count before handshake init
	Jmin int    // min junk packet size
	Jmax int    // max junk packet size
	S1   int    // padding bytes prepended to handshake init
	S2   int    // padding bytes prepended to handshake response
	H1   uint32 // replacement type for handshake init
	H2   uint32 // replacement type for handshake response
	H3   uint32 // replacement type for cookie reply
	H4   uint32 // replacement type for transport data

	ServerPub     [32]byte // AWG server public key (for outbound MAC1 recomputation)
	ClientPub     [32]byte // WG client public key (for inbound MAC1 recomputation)
	mac1keyServer [32]byte // precomputed BLAKE2s-256("mac1----" || ServerPub)
	mac1keyClient [32]byte // precomputed BLAKE2s-256("mac1----" || ClientPub)

	Timeout  int // inactivity timeout seconds, default 180
	LogLevel int // 0=none, 1=error, 2=info
}

// Log levels.
const (
	LevelNone  = 0
	LevelError = 1
	LevelInfo  = 2
	LevelDebug = 3
)

// ComputeMAC1Keys derives MAC1 keys from ServerPub and ClientPub.
func (c *Config) ComputeMAC1Keys() {
	c.mac1keyServer = computeMAC1Key(c.ServerPub)
	c.mac1keyClient = computeMAC1Key(c.ClientPub)
}

// TransformOutbound transforms an outbound WireGuard packet into AmneziaWG format.
// It returns the transformed packet and whether junk packets should be sent before it.
// The input buffer must not be reused after this call if padding is applied.
func TransformOutbound(buf []byte, n int, cfg *Config) (out []byte, sendJunk bool) {
	if n < 4 {
		return buf[:n], false
	}

	msgType := binary.LittleEndian.Uint32(buf[:4])

	switch {
	case msgType == wgHandshakeInit && n == WgHandshakeInitSize:
		// Replace type and recompute MAC1.
		binary.LittleEndian.PutUint32(buf[:4], cfg.H1)
		if cfg.ServerPub != ([32]byte{}) {
			recomputeMAC1(buf[:n], cfg.mac1keyServer)
		}
		if cfg.S1 > 0 {
			out = make([]byte, cfg.S1+n)
			randFill(out[:cfg.S1])
			copy(out[cfg.S1:], buf[:n])
		} else {
			out = buf[:n]
		}
		return out, cfg.Jc > 0

	case msgType == wgHandshakeResponse && n == WgHandshakeResponseSize:
		// Replace type and prepend S2 padding bytes.
		binary.LittleEndian.PutUint32(buf[:4], cfg.H2)
		if cfg.S2 > 0 {
			out = make([]byte, cfg.S2+n)
			randFill(out[:cfg.S2])
			copy(out[cfg.S2:], buf[:n])
		} else {
			out = buf[:n]
		}
		return out, false

	case msgType == wgCookieReply && n == WgCookieReplySize:
		// Replace type, no padding.
		binary.LittleEndian.PutUint32(buf[:4], cfg.H3)
		return buf[:n], false

	case msgType == wgTransportData && n >= WgTransportMinSize:
		// Hot path: replace type in-place, no allocation.
		binary.LittleEndian.PutUint32(buf[:4], cfg.H4)
		return buf[:n], false

	default:
		// Unknown packet, pass through unchanged.
		return buf[:n], false
	}
}

// TransformInbound transforms an inbound AmneziaWG packet back to standard WireGuard format.
// Returns the transformed packet and whether it is valid (junk packets return valid=false).
func TransformInbound(buf []byte, n int, cfg *Config) (out []byte, valid bool) {
	if n < 4 {
		return nil, false
	}

	// Check for handshake init with S1 padding: total size = S1 + 148.
	if cfg.S1 > 0 && n == cfg.S1+WgHandshakeInitSize {
		offset := cfg.S1
		if n < offset+4 {
			return nil, false
		}
		msgType := binary.LittleEndian.Uint32(buf[offset : offset+4])
		if msgType == cfg.H1 {
			binary.LittleEndian.PutUint32(buf[offset:offset+4], wgHandshakeInit)
			return buf[offset:n], true
		}
	}

	// Check for handshake init without padding (S1=0).
	if cfg.S1 == 0 && n == WgHandshakeInitSize {
		msgType := binary.LittleEndian.Uint32(buf[:4])
		if msgType == cfg.H1 {
			binary.LittleEndian.PutUint32(buf[:4], wgHandshakeInit)
			return buf[:n], true
		}
	}

	// Check for handshake response with S2 padding: total size = S2 + 92.
	if cfg.S2 > 0 && n == cfg.S2+WgHandshakeResponseSize {
		offset := cfg.S2
		if n < offset+4 {
			return nil, false
		}
		msgType := binary.LittleEndian.Uint32(buf[offset : offset+4])
		if msgType == cfg.H2 {
			binary.LittleEndian.PutUint32(buf[offset:offset+4], wgHandshakeResponse)
			if cfg.ClientPub != ([32]byte{}) {
				recomputeMAC1Response(buf[offset:offset+WgHandshakeResponseSize], cfg.mac1keyClient)
			}
			return buf[offset:n], true
		}
	}

	// Check for handshake response without padding (S2=0).
	if cfg.S2 == 0 && n == WgHandshakeResponseSize {
		msgType := binary.LittleEndian.Uint32(buf[:4])
		if msgType == cfg.H2 {
			binary.LittleEndian.PutUint32(buf[:4], wgHandshakeResponse)
			if cfg.ClientPub != ([32]byte{}) {
				recomputeMAC1Response(buf[:WgHandshakeResponseSize], cfg.mac1keyClient)
			}
			return buf[:n], true
		}
	}

	// Cookie reply: no padding, fixed size.
	if n == WgCookieReplySize {
		msgType := binary.LittleEndian.Uint32(buf[:4])
		if msgType == cfg.H3 {
			binary.LittleEndian.PutUint32(buf[:4], wgCookieReply)
			return buf[:n], true
		}
	}

	// Transport data: no padding, variable size >= 32.
	if n >= WgTransportMinSize {
		msgType := binary.LittleEndian.Uint32(buf[:4])
		if msgType == cfg.H4 {
			binary.LittleEndian.PutUint32(buf[:4], wgTransportData)
			return buf[:n], true
		}
	}

	// Unknown or junk packet.
	return nil, false
}

// GenerateJunkPackets creates Jc junk packets with random sizes in [Jmin, Jmax].
func GenerateJunkPackets(cfg *Config) [][]byte {
	if cfg.Jc <= 0 || cfg.Jmax <= 0 {
		return nil
	}

	jmin := cfg.Jmin
	if jmin <= 0 {
		jmin = 1
	}
	jmax := cfg.Jmax
	if jmax < jmin {
		jmax = jmin
	}

	packets := make([][]byte, cfg.Jc)
	for i := range packets {
		size := jmin
		if jmax > jmin {
			size = jmin + rand.IntN(jmax-jmin+1)
		}
		pkt := make([]byte, size)
		randFill(pkt)
		packets[i] = pkt
	}
	return packets
}
