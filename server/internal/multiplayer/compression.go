package multiplayer

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"kairisei.local/server/internal/wirecompression"
)

const binaryGZIPCapability = "gzip-v2"
const binaryGZIPMagic = "\x00GZ2"
const binaryGZIPHeaderSize = 12 // magic + uncompressed length + compressed length, big endian

type frameCompression uint8

const (
	compressionNone frameCompression = iota
	compressionGZIPBinary
)

// Acknowledgement and mode change share writeMu, so no compressed frame can
// overtake the acknowledgement.
func (c *clientConn) handlePing(payload string) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	mode := compressionNone
	if payload == binaryGZIPCapability {
		mode = compressionGZIPBinary
	}
	if mode != compressionNone && c.compression == compressionNone {
		if err := c.writeFrameLocked("LocalCompression", payload); err != nil {
			return err
		}
		c.compression = mode
	}
	return c.writeFrameLocked("Pong", strconv.FormatInt(time.Now().Unix(), 10))
}

// One immutable message per broadcast, compressed at most once outside state locks.
// Fanout to raw and compressed peers preserves the exact same original frame.
type preparedBattleFrame struct {
	frame                           battleFrame
	rawOnce, packedOnce, binaryOnce sync.Once
	raw, binary                     string
	packed                          []byte
	err                             error
}

func prepareBattleFrames(frames []battleFrame) []*preparedBattleFrame {
	prepared := make([]*preparedBattleFrame, len(frames))
	for i, frame := range frames {
		prepared[i] = &preparedBattleFrame{frame: frame}
	}
	return prepared
}
func (p *preparedBattleFrame) bytes(mode frameCompression) (string, string, error) {
	p.rawOnce.Do(func() {
		if p.frame.method == "" || strings.ContainsAny(p.frame.method, "{}\r\n") {
			p.err = errors.New("BattleSv response method is invalid")
			return
		}
		p.raw = p.frame.method + "{\n"
		if p.frame.payload != "" {
			p.raw += p.frame.payload + "\n"
		}
		p.raw += "}\n"
		if len(p.raw) > maxFrameBytes || !utf8.ValidString(p.raw) {
			p.err = errors.New("BattleSv response frame is invalid or too large")
		}
	})
	if p.err != nil {
		return "", "", p.err
	}
	if mode == compressionNone || len(p.raw) < wirecompression.Threshold {
		return p.raw, p.raw, nil
	}
	p.packedOnce.Do(func() {
		p.packed, _ = wirecompression.Compress([]byte(p.raw))
	})
	if len(p.packed) == 0 {
		return p.raw, p.raw, nil // The original frame is valid if compression fails.
	}
	p.binaryOnce.Do(func() {
		p.binary = p.raw
		if binaryGZIPHeaderSize+len(p.packed)+64 >= len(p.raw) {
			return
		}
		frame := make([]byte, binaryGZIPHeaderSize+len(p.packed))
		copy(frame, binaryGZIPMagic)
		binary.BigEndian.PutUint32(frame[4:8], uint32(len(p.raw)))
		binary.BigEndian.PutUint32(frame[8:12], uint32(len(p.packed)))
		copy(frame[binaryGZIPHeaderSize:], p.packed)
		p.binary = string(frame)
	})
	return p.raw, p.binary, nil
}

func (c *clientConn) writePreparedFrameLocked(frame *preparedBattleFrame) error {
	if c.closed {
		return net.ErrClosed
	}
	raw, wire, err := frame.bytes(c.compression)
	if err != nil {
		return err
	}
	start := time.Now()
	_ = c.conn.SetWriteDeadline(start.Add(10 * time.Second))
	n, err := io.WriteString(c.conn, wire)
	if err == nil && n != len(wire) {
		err = io.ErrShortWrite
	}
	if c.server != nil {
		c.server.downloadBytes.Add(c.server.logger, "battlesv_download", len(raw), n, time.Since(start))
	}
	if err != nil {
		_ = c.conn.Close()
	}
	return err
}

// Text frames remain valid after negotiation. The leading zero distinguishes
// a length-delimited gzip envelope without changing any business method/CSV.
func (c *clientConn) readFrame(reader *bufio.Reader) (string, string, error) {
	first, err := reader.Peek(1)
	if err != nil {
		return "", "", err
	}
	if first[0] != 0 {
		return readFrame(reader)
	}
	if c.compression != compressionGZIPBinary {
		return "", "", errors.New("unnegotiated binary compressed frame")
	}
	var header [binaryGZIPHeaderSize]byte
	if _, err := io.ReadFull(reader, header[:]); err != nil {
		return "", "", err
	}
	size := binary.BigEndian.Uint32(header[4:8])
	packedSize := binary.BigEndian.Uint32(header[8:12])
	if string(header[:4]) != binaryGZIPMagic || size < 4 || size > maxFrameBytes || packedSize < 20 || packedSize > maxFrameBytes {
		return "", "", errors.New("invalid binary compressed frame header")
	}
	packed := make([]byte, int(packedSize))
	if _, err := io.ReadFull(reader, packed); err != nil {
		return "", "", err
	}
	return decodeGZIPBytes(packed, int(size))
}

func decodeGZIPBytes(packed []byte, size int) (string, string, error) {
	raw, err := wirecompression.Decompress(packed, size)
	if err != nil || len(raw) != size || !utf8.Valid(raw) || raw[0] == 0 {
		return "", "", errors.New("invalid compressed frame contents")
	}
	reader := bufio.NewReader(bytes.NewReader(raw))
	method, body, err := readFrame(reader)
	if err != nil {
		return "", "", err
	}
	if _, err = reader.ReadByte(); err != io.EOF || method == "LocalGzip1" || method == "LocalCompression" {
		return "", "", errors.New("nested or trailing compressed frame")
	}
	return method, body, nil
}
