package multiplayer

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"kairisei.local/server/internal/wirecompression"
)

type fragmentedCompressionReader struct {
	io.Reader
	width int
}

func (r fragmentedCompressionReader) Read(p []byte) (int, error) {
	return r.Reader.Read(p[:min(len(p), r.width)])
}

func TestBinaryBattleCompression(t *testing.T) {
	frame := battleFrame{"ApiUserAttack", strings.Repeat("601001,0,1,2345,日本\n", 400) + "tail\n"}
	p := &preparedBattleFrame{frame: frame}
	raw, wire, err := p.bytes(compressionGZIPBinary)
	if err != nil || !strings.HasPrefix(wire, binaryGZIPMagic) || len(wire) >= len(raw) {
		t.Fatal("binary frame not compressed", err)
	}
	for _, width := range []int{1, 2, 7, 4096} {
		c := &clientConn{compression: compressionGZIPBinary}
		reader := bufio.NewReader(fragmentedCompressionReader{strings.NewReader(wire + "Pong{\n123\n}\n"), width})
		method, body, err := c.readFrame(reader)
		if err != nil || method != frame.method || body != frame.payload {
			t.Fatalf("fragmented binary frame changed at width %d: %v", width, err)
		}
		method, body, err = c.readFrame(reader)
		if err != nil || method != "Pong" || body != "123" {
			t.Fatal("binary frame consumed the following text frame", err)
		}
	}
	if _, _, err := (&clientConn{}).readFrame(bufio.NewReader(strings.NewReader(wire))); err == nil {
		t.Fatal("binary frame accepted before negotiation")
	}
	badCRC := []byte(wire)
	badCRC[len(badCRC)-8] ^= 1
	tooLarge := []byte(wire)
	binary.BigEndian.PutUint32(tooLarge[4:8], maxFrameBytes+1)
	packedTooLarge := []byte(wire)
	binary.BigEndian.PutUint32(packedTooLarge[8:12], maxFrameBytes+1)
	badMagic := []byte(wire)
	badMagic[1] = 'X'
	joined := append([]byte(wire), []byte(wire[binaryGZIPHeaderSize:])...)
	binary.BigEndian.PutUint32(joined[8:12], uint32(len(joined)-binaryGZIPHeaderSize))
	for _, bad := range [][]byte{badCRC, tooLarge, packedTooLarge, badMagic, joined, []byte(wire[:len(wire)-1])} {
		if _, _, err := (&clientConn{compression: compressionGZIPBinary}).readFrame(bufio.NewReader(bytes.NewReader(bad))); err == nil {
			t.Fatal("accepted damaged binary envelope")
		}
	}
	conn, peer := net.Pipe()
	defer conn.Close()
	defer peer.Close()
	_ = peer.SetDeadline(time.Now().Add(3 * time.Second))
	c := &clientConn{server: &Server{logger: slog.New(slog.NewTextHandler(io.Discard, nil))}, conn: conn}
	done := make(chan error, 1)
	go func() { done <- c.handlePing(binaryGZIPCapability) }()
	r := bufio.NewReader(peer)
	method, body, err := readFrame(r)
	if err != nil || method != "LocalCompression" || body != binaryGZIPCapability {
		t.Fatal("binary capability acknowledgement", err)
	}
	method, _, err = readFrame(r)
	if err != nil || method != "Pong" {
		t.Fatal("Pong ordering changed", err)
	}
	if err := <-done; err != nil || c.compression != compressionGZIPBinary {
		t.Fatal("binary negotiation failed", err)
	}
}

func TestBinaryBattleCompressionClientVectors(t *testing.T) {
	root := os.Getenv("CN602_BINARY_COMPRESSION_VECTORS")
	if root == "" {
		t.Skip("cross-runtime vectors not requested")
	}
	root, err := filepath.Abs(root)
	if err != nil || !strings.Contains(filepath.ToSlash(root), "/_local/transient/") {
		t.Fatal("vectors must be project transient")
	}
	if err := os.MkdirAll(root, 0755); err != nil {
		t.Fatal(err)
	}
	frames := []battleFrame{{"One", ""}, {"One", strings.Repeat("日本／🙂,12,34\n", 500) + "tail"}, {"Two", strings.Repeat("123,4,56,789\n", 4000) + "end\n"}, {"One", strings.Repeat("x", maxFrameBytes-8)}}
	for i, frame := range frames {
		raw, wire, err := (&preparedBattleFrame{frame: frame}).bytes(compressionGZIPBinary)
		if err != nil {
			t.Fatal(err)
		}
		stem := filepath.Join(root, strconv.Itoa(i))
		for ext, value := range map[string]string{".raw": raw, ".go-bin": wire} {
			name := stem + ext
			if before, err := os.ReadFile(name); err == nil {
				if string(before) != value {
					t.Fatal("vector changed", name)
				}
				continue
			}
			if err := os.WriteFile(name, []byte(value), 0600); err != nil {
				t.Fatal(err)
			}
		}
		if incoming, err := os.ReadFile(stem + ".client-bin"); err == nil {
			reader := bufio.NewReader(bytes.NewReader(incoming))
			method, body, err := (&clientConn{compression: compressionGZIPBinary}).readFrame(reader)
			if err != nil || method != frame.method || body != frame.payload {
				t.Fatalf("Unity client frame changed %d: %v", i, err)
			}
			if _, err := reader.ReadByte(); err != io.EOF {
				t.Fatal("trailing client envelope bytes")
			}
		}
	}
	if packed, err := os.ReadFile(filepath.Join(root, "http-upload.gz")); err == nil {
		raw, err := wirecompression.Decompress(packed, 1<<20)
		want, readErr := os.ReadFile(filepath.Join(root, "http-upload.raw"))
		if err != nil || readErr != nil || !bytes.Equal(raw, want) {
			t.Fatal("Unity HTTP upload changed original authentication/body bytes", err, readErr)
		}
	}
}

func TestCompressedBattleFrames(t *testing.T) {
	frames := []battleFrame{{"Chat", "繁體／日本語／🙂"}, {"Empty", ""}, {"ApiUserAttack", strings.Repeat("601001,0,1,2345\n", 500) + "601001,0,0,0"}}
	for _, frame := range frames {
		p := &preparedBattleFrame{frame: frame}
		raw, wire, err := p.bytes(compressionGZIPBinary)
		if err != nil {
			t.Fatal(err)
		}
		method, payload, err := (&clientConn{compression: compressionGZIPBinary}).readFrame(bufio.NewReader(strings.NewReader(wire)))
		if err != nil || method != frame.method || payload != frame.payload {
			t.Fatalf("decoded frame changed: %s %v", frame.method, err)
		}
		if len(raw) >= 1024 && len(wire) >= len(raw) {
			t.Fatal("large fixture did not compress")
		}
	}
	raw := []byte("ApiUserAttack{\n" + strings.Repeat("row\n", 400) + "}\n")
	packed, _ := wirecompression.Compress(raw)
	corrupt := append([]byte(nil), packed...)
	corrupt[len(corrupt)-8] ^= 1
	joined := append(append([]byte(nil), packed...), packed...)
	for _, bad := range []struct {
		data []byte
		size int
	}{{corrupt, len(raw)}, {packed[:len(packed)-2], len(raw)}, {joined, len(raw)}, {packed, len(raw) - 1}, {packed, maxFrameBytes + 1}} {
		if _, _, err := decodeGZIPBytes(bad.data, bad.size); err == nil {
			t.Fatal("accepted corrupt/oversized frame")
		}
	}
	for _, invalid := range []string{"One{\n\xff\n}\n", "One{\n1\n}\nTwo{\n2\n}\n", "LocalCompression{\ngzip-v2\n}\n", "LocalGzip1{\ninvalid\n}\n"} {
		compressed, _ := wirecompression.Compress([]byte(invalid))
		if _, _, err := decodeGZIPBytes(compressed, len(invalid)); err == nil {
			t.Fatal("accepted invalid UTF8, nested control or multiple business frames")
		}
	}
}
