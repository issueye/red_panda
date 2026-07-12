package ipc

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"sync"
)

// DefaultMaxLineBytes is the default maximum length of a single framed line.
// Matches the gateway/agent JSON-RPC line budget (4 MiB).
const DefaultMaxLineBytes = 4 * 1024 * 1024

// Stream provides newline-delimited message framing over a bidirectional
// connection. It mirrors the previous stdio NDJSON convention: one complete
// message per line, no embedded newlines in payloads.
//
// Stream is safe for concurrent use of ReadLine from one goroutine and
// WriteLine from another, matching typical JSON-RPC client/server patterns.
// Concurrent WriteLine calls are serialized; concurrent ReadLine is not.
type Stream struct {
	conn   net.Conn
	reader *bufio.Reader
	max    int

	writeMu sync.Mutex
}

// NewStream wraps conn with NDJSON line framing.
// maxLineBytes <= 0 selects DefaultMaxLineBytes.
func NewStream(conn net.Conn, maxLineBytes int) *Stream {
	if maxLineBytes <= 0 {
		maxLineBytes = DefaultMaxLineBytes
	}
	return &Stream{
		conn:   conn,
		reader: bufio.NewReaderSize(conn, 64*1024),
		max:    maxLineBytes,
	}
}

// Conn returns the underlying connection.
func (s *Stream) Conn() net.Conn {
	return s.conn
}

// ReadLine reads one message (bytes up to but excluding the trailing '\n').
// Returns io.EOF when the peer closes the connection cleanly.
func (s *Stream) ReadLine() ([]byte, error) {
	if s == nil || s.reader == nil {
		return nil, fmt.Errorf("ipc: stream is closed")
	}
	var (
		buf   []byte
		total int
	)
	for {
		chunk, err := s.reader.ReadSlice('\n')
		if len(chunk) > 0 {
			// Drop the delimiter when present.
			n := len(chunk)
			if chunk[n-1] == '\n' {
				n--
				if n > 0 && chunk[n-1] == '\r' {
					n--
				}
				if total == 0 && err == nil {
					// Fast path: whole line in one slice.
					out := make([]byte, n)
					copy(out, chunk[:n])
					return out, nil
				}
				buf = append(buf, chunk[:n]...)
				total += n
				if total > s.max {
					return nil, fmt.Errorf("ipc: line exceeds %d bytes", s.max)
				}
				return buf, nil
			}
			buf = append(buf, chunk...)
			total += len(chunk)
			if total > s.max {
				return nil, fmt.Errorf("ipc: line exceeds %d bytes", s.max)
			}
		}
		if err == bufio.ErrBufferFull {
			continue
		}
		if err != nil {
			if err == io.EOF && total > 0 {
				// Incomplete final line without newline.
				return buf, io.ErrUnexpectedEOF
			}
			return nil, err
		}
	}
}

// WriteLine writes one message followed by '\n'.
func (s *Stream) WriteLine(line []byte) error {
	if s == nil || s.conn == nil {
		return fmt.Errorf("ipc: stream is closed")
	}
	if len(line) > s.max {
		return fmt.Errorf("ipc: line exceeds %d bytes", s.max)
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	// Single write when possible: payload + '\n'.
	buf := make([]byte, len(line)+1)
	copy(buf, line)
	buf[len(line)] = '\n'
	_, err := s.conn.Write(buf)
	return err
}

// WriteStringLine is WriteLine for string payloads.
func (s *Stream) WriteStringLine(line string) error {
	return s.WriteLine([]byte(line))
}

// Close closes the underlying connection.
func (s *Stream) Close() error {
	if s == nil || s.conn == nil {
		return nil
	}
	return s.conn.Close()
}

// Reader exposes a line-oriented io.Reader view for APIs that still want
// to scan an io.Reader (e.g. bufio.Scanner over the stream). Prefer ReadLine
// for new code. The returned reader is the same connection; do not mix
// Scanner and ReadLine on the same Stream.
func (s *Stream) Reader() io.Reader {
	return s.conn
}

// Writer returns an io.Writer that writes raw bytes to the connection
// without framing. Prefer WriteLine for NDJSON messages.
func (s *Stream) Writer() io.Writer {
	return &lockedWriter{mu: &s.writeMu, w: s.conn}
}

type lockedWriter struct {
	mu *sync.Mutex
	w  io.Writer
}

func (w *lockedWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.w.Write(p)
}
