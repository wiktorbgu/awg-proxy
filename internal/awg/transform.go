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

// HRange represents a uint32 range [Min, Max] for v2 H-parameters.
type HRange struct {
	Min, Max uint32
}

// Pick returns a random value in [Min, Max]. Returns Min if Min == Max.
func (r HRange) Pick() uint32 {
	if r.Min == r.Max {
		return r.Min
	}
	return r.Min + uint32(rand.IntN(int(r.Max-r.Min+1)))
}

// Contains returns true if v is in [Min, Max].
func (r HRange) Contains(v uint32) bool {
	return v >= r.Min && v <= r.Max
}

// Config holds AmneziaWG obfuscation parameters.
type Config struct {
	Jc   int    // junk packet count before handshake init
	Jmin int    // min junk packet size
	Jmax int    // max junk packet size
	S1   int    // padding bytes prepended to handshake init
	S2   int    // padding bytes prepended to handshake response
	S3   int    // padding bytes prepended to cookie reply (v2, default 0)
	S4   int    // padding bytes prepended to transport data (v2, default 0)
	H1   HRange // replacement type for handshake init
	H2   HRange // replacement type for handshake response
	H3   HRange // replacement type for cookie reply
	H4   HRange // replacement type for transport data

	CPS [5]*CPSTemplate // I1-I5 CPS templates (v2, nil = not configured)

	ServerPub     [32]byte // AWG server public key (for outbound MAC1 recomputation)
	ClientPub     [32]byte // WG client public key (for inbound MAC1 recomputation)
	mac1keyServer [32]byte // precomputed BLAKE2s-256("mac1----" || ServerPub)
	mac1keyClient [32]byte // precomputed BLAKE2s-256("mac1----" || ClientPub)

	h4Fixed uint32 // H4.Min for point-range configs (avoids Pick())
	h4NoOp  bool   // true when H4={4,4} and S4==0 (identity transform, zero work)
	maxScan int    // precomputed max(S1,S2,S3,S4) for inbound scanning

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

// ComputeFastPath precomputes fast-path flags for hot-path optimizations.
// Must be called after setting H4, S4 and all S1-S4 values.
func (c *Config) ComputeFastPath() {
	c.h4Fixed = c.H4.Min
	c.h4NoOp = c.H4.Min == wgTransportData && c.H4.Max == wgTransportData && c.S4 == 0
	c.maxScan = max(c.S1, c.S2, c.S3, c.S4)
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
		binary.LittleEndian.PutUint32(buf[:4], cfg.H1.Pick())
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
		binary.LittleEndian.PutUint32(buf[:4], cfg.H2.Pick())
		if cfg.S2 > 0 {
			out = make([]byte, cfg.S2+n)
			randFill(out[:cfg.S2])
			copy(out[cfg.S2:], buf[:n])
		} else {
			out = buf[:n]
		}
		return out, false

	case msgType == wgCookieReply && n == WgCookieReplySize:
		binary.LittleEndian.PutUint32(buf[:4], cfg.H3.Pick())
		if cfg.S3 > 0 {
			out = make([]byte, cfg.S3+n)
			randFill(out[:cfg.S3])
			copy(out[cfg.S3:], buf[:n])
		} else {
			out = buf[:n]
		}
		return out, false

	case msgType == wgTransportData && n >= WgTransportMinSize:
		if cfg.h4NoOp {
			return buf[:n], false
		}
		if cfg.H4.Min == cfg.H4.Max {
			binary.LittleEndian.PutUint32(buf[:4], cfg.h4Fixed)
		} else {
			binary.LittleEndian.PutUint32(buf[:4], cfg.H4.Pick())
		}
		if cfg.S4 > 0 {
			out = make([]byte, cfg.S4+n)
			randFill(out[:cfg.S4])
			copy(out[cfg.S4:], buf[:n])
		} else {
			out = buf[:n]
		}
		return out, false

	default:
		// Unknown packet, pass through unchanged.
		return buf[:n], false
	}
}

// TransformInbound transforms an inbound AmneziaWG packet back to standard WireGuard format.
// Returns the transformed packet and whether it is valid (junk packets return valid=false).
// Uses scanning to find the header at offsets [0, maxScan] to handle S1-S4 padding.
func TransformInbound(buf []byte, n int, cfg *Config) (out []byte, valid bool) {
	if n < 4 {
		return nil, false
	}

	// Fast path: transport data at offset 0 (no S4 padding, ~99.9% of traffic).
	h := binary.LittleEndian.Uint32(buf[:4])
	if cfg.H4.Contains(h) && n >= WgTransportMinSize {
		if !cfg.h4NoOp {
			binary.LittleEndian.PutUint32(buf[:4], wgTransportData)
		}
		return buf[:n], true
	}

	// Slow path: scan offsets, check H4 first (most frequent), then H1/H2/H3.
	for off := 0; off <= cfg.maxScan && off+4 <= n; off++ {
		h := binary.LittleEndian.Uint32(buf[off : off+4])
		rem := n - off

		if cfg.H4.Contains(h) && rem >= WgTransportMinSize {
			binary.LittleEndian.PutUint32(buf[off:off+4], wgTransportData)
			return buf[off:n], true
		}
		if cfg.H1.Contains(h) && rem == WgHandshakeInitSize {
			binary.LittleEndian.PutUint32(buf[off:off+4], wgHandshakeInit)
			return buf[off:n], true
		}
		if cfg.H2.Contains(h) && rem == WgHandshakeResponseSize {
			binary.LittleEndian.PutUint32(buf[off:off+4], wgHandshakeResponse)
			if cfg.ClientPub != ([32]byte{}) {
				recomputeMAC1Response(buf[off:off+WgHandshakeResponseSize], cfg.mac1keyClient)
			}
			return buf[off:n], true
		}
		if cfg.H3.Contains(h) && rem == WgCookieReplySize {
			binary.LittleEndian.PutUint32(buf[off:off+4], wgCookieReply)
			return buf[off:n], true
		}
	}

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
