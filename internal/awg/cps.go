package awg

import (
	"encoding/binary"
	"errors"
	"strconv"
	"time"
)

// CPS template segment kinds.
const (
	cpsStatic    byte = 'b' // static hex bytes
	cpsRandom    byte = 'r' // random bytes
	cpsTimestamp byte = 't' // 4-byte LE unix timestamp
	cpsCounter   byte = 'c' // 4-byte LE packet counter
)

type cpsSegment struct {
	kind byte
	data []byte // static bytes for 'b'
	size int    // byte count for 'r'
}

// CPSTemplate represents a parsed CPS template (I1-I5).
type CPSTemplate struct {
	segments []cpsSegment
}

// ParseCPSTemplate parses a CPS template string.
// Format tags: <b 0xHEX>, <r SIZE>, <t>, <c>
func ParseCPSTemplate(s string) (*CPSTemplate, error) {
	var segs []cpsSegment
	i := 0
	for i < len(s) {
		// Skip whitespace between tags.
		if s[i] == ' ' || s[i] == '\t' || s[i] == '\n' || s[i] == '\r' {
			i++
			continue
		}
		if s[i] != '<' {
			return nil, errors.New("expected '<' at position " + strconv.Itoa(i))
		}
		// Find closing '>'.
		end := -1
		for j := i + 1; j < len(s); j++ {
			if s[j] == '>' {
				end = j
				break
			}
		}
		if end < 0 {
			return nil, errors.New("unclosed '<' at position " + strconv.Itoa(i))
		}
		inner := s[i+1 : end]
		seg, err := parseCPSTag(inner)
		if err != nil {
			return nil, err
		}
		segs = append(segs, seg)
		i = end + 1
	}
	if len(segs) == 0 {
		return nil, errors.New("empty CPS template")
	}
	return &CPSTemplate{segments: segs}, nil
}

func parseCPSTag(tag string) (cpsSegment, error) {
	if len(tag) == 0 {
		return cpsSegment{}, errors.New("empty tag")
	}
	kind := tag[0]
	switch kind {
	case 'b':
		// <b 0xHEXDATA>
		rest := trimLeft(tag[1:])
		if len(rest) < 3 || rest[0] != '0' || (rest[1] != 'x' && rest[1] != 'X') {
			return cpsSegment{}, errors.New("expected '0x' prefix in <b> tag")
		}
		hex := rest[2:]
		data, err := decodeHex(hex)
		if err != nil {
			return cpsSegment{}, err
		}
		return cpsSegment{kind: cpsStatic, data: data}, nil

	case 'r':
		// <r SIZE>
		rest := trimLeft(tag[1:])
		size, err := strconv.Atoi(rest)
		if err != nil {
			return cpsSegment{}, errors.New("invalid size in <r> tag: " + err.Error())
		}
		if size <= 0 {
			return cpsSegment{}, errors.New("<r> size must be positive")
		}
		return cpsSegment{kind: cpsRandom, size: size}, nil

	case 't':
		return cpsSegment{kind: cpsTimestamp}, nil

	case 'c':
		return cpsSegment{kind: cpsCounter}, nil

	default:
		return cpsSegment{}, errors.New("unknown tag kind: " + string(kind))
	}
}

// trimLeft removes leading spaces/tabs.
func trimLeft(s string) string {
	i := 0
	for i < len(s) && (s[i] == ' ' || s[i] == '\t') {
		i++
	}
	return s[i:]
}

// Generate builds a CPS packet from the template.
func (t *CPSTemplate) Generate(counter uint32) []byte {
	// Calculate total size.
	total := 0
	for _, seg := range t.segments {
		switch seg.kind {
		case cpsStatic:
			total += len(seg.data)
		case cpsRandom:
			total += seg.size
		case cpsTimestamp:
			total += 4
		case cpsCounter:
			total += 4
		}
	}

	buf := make([]byte, total)
	off := 0
	for _, seg := range t.segments {
		switch seg.kind {
		case cpsStatic:
			copy(buf[off:], seg.data)
			off += len(seg.data)
		case cpsRandom:
			randFill(buf[off : off+seg.size])
			off += seg.size
		case cpsTimestamp:
			binary.LittleEndian.PutUint32(buf[off:], uint32(time.Now().Unix()))
			off += 4
		case cpsCounter:
			binary.LittleEndian.PutUint32(buf[off:], counter)
			off += 4
		}
	}
	return buf
}

// GenerateCPSPackets generates all configured CPS packets (I1->I5 order).
// counter is incremented for each packet sent.
func GenerateCPSPackets(templates [5]*CPSTemplate, counter *uint32) [][]byte {
	var packets [][]byte
	for _, tmpl := range templates {
		if tmpl == nil {
			continue
		}
		pkt := tmpl.Generate(*counter)
		*counter++
		packets = append(packets, pkt)
	}
	return packets
}

// decodeHex decodes a hex string to bytes. Hand-written, no encoding/hex dependency.
func decodeHex(s string) ([]byte, error) {
	if len(s)%2 != 0 {
		return nil, errors.New("odd-length hex string")
	}
	out := make([]byte, len(s)/2)
	for i := 0; i < len(s); i += 2 {
		hi := hexVal(s[i])
		lo := hexVal(s[i+1])
		if hi < 0 || lo < 0 {
			return nil, errors.New("invalid hex char at position " + strconv.Itoa(i))
		}
		out[i/2] = byte(hi<<4 | lo)
	}
	return out, nil
}

// hexVal returns the value of a hex character, or -1 if invalid.
func hexVal(c byte) int {
	switch {
	case c >= '0' && c <= '9':
		return int(c - '0')
	case c >= 'a' && c <= 'f':
		return int(c-'a') + 10
	case c >= 'A' && c <= 'F':
		return int(c-'A') + 10
	default:
		return -1
	}
}
