package main

import (
	"bufio"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Live reload over WebSocket, spoken by hand.
//
// Go's standard library has no WebSocket. net/http carries the handshake's HTTP
// half -- it is an ordinary GET with an Upgrade header -- and then stops, so the
// framing in this file is RFC 6455 implemented directly.
//
// An honest note, because it belongs in the record rather than in a footnote:
// Server-Sent Events would have done this job in about twenty lines using
// nothing but net/http and a Flush, and for one-way reload notifications SSE is
// arguably the better engineering. WebSocket is here because gorilla/websocket
// is a dependency worth deleting outright, and because doing it correctly is
// only about 150 lines. The choice was deliberate, not uninformed.

// websocketGUID is the constant RFC 6455 section 4.2.2 requires appended to the
// client's key before hashing.
//
// Verified against the worked example in section 1.3 rather than trusted: the
// RFC publishes both the SHA-1 digest and the resulting accept value for a known
// key, and TestAcceptKey asserts this constant reproduces them. It is worth the
// test. The first version of this line had the last group as "5AB0DC85B11A"
// instead of "C5AB0DC85B11" -- one character adrift, a valid-looking GUID, and
// no browser on earth would have completed the handshake.
const websocketGUID = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"

// WebSocket opcodes, from RFC 6455 section 5.2.
const (
	opText  = 0x1
	opClose = 0x8
	opPing  = 0x9
	opPong  = 0xA
)

// liveHub holds the set of connected browsers.
//
// Concurrency model: the hub's map is guarded by hub.mu, and each client owns a
// separate write mutex. Two locks rather than one, because a broadcast must not
// hold the map lock while writing to a slow socket -- that would let one stalled
// browser block every other reload. The map lock is held only long enough to
// copy the client list.
type liveHub struct {
	mu      sync.Mutex
	clients map[*liveClient]struct{}
}

type liveClient struct {
	conn net.Conn
	rw   *bufio.ReadWriter
	mu   sync.Mutex // serialises frame writes; a frame must not interleave
}

func newLiveHub() *liveHub {
	return &liveHub{clients: make(map[*liveClient]struct{})}
}

// Handle upgrades an HTTP request to a WebSocket and serves it until it closes.
func (h *liveHub) Handle(w http.ResponseWriter, r *http.Request) {
	client, err := upgrade(w, r)
	if err != nil {
		http.Error(w, "websocket upgrade failed", http.StatusBadRequest)
		return
	}

	h.mu.Lock()
	h.clients[client] = struct{}{}
	h.mu.Unlock()

	defer func() {
		h.mu.Lock()
		delete(h.clients, client)
		h.mu.Unlock()
		client.conn.Close()
	}()

	// Read until the peer goes away. Nothing the browser sends is interesting
	// except close and ping, but the frames have to be drained regardless or
	// the socket's buffer fills.
	client.readLoop()
}

// Broadcast sends one text message to every connected browser.
func (h *liveHub) Broadcast(msg string) {
	h.mu.Lock()
	clients := make([]*liveClient, 0, len(h.clients))
	for c := range h.clients {
		clients = append(clients, c)
	}
	h.mu.Unlock()

	for _, c := range clients {
		if err := c.writeFrame(opText, []byte(msg)); err != nil {
			// A failed write means the browser is gone; the read loop will
			// notice and deregister it.
			c.conn.Close()
		}
	}
}

// Clients reports how many browsers are connected, for the dev server's log.
func (h *liveHub) Clients() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.clients)
}

// upgrade performs the RFC 6455 opening handshake and hijacks the connection.
func upgrade(w http.ResponseWriter, r *http.Request) (*liveClient, error) {
	if !strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
		return nil, errors.New("not a websocket upgrade")
	}
	if !headerContainsToken(r.Header.Get("Connection"), "upgrade") {
		return nil, errors.New("missing Connection: Upgrade")
	}
	if r.Header.Get("Sec-WebSocket-Version") != "13" {
		return nil, errors.New("unsupported websocket version")
	}
	key := r.Header.Get("Sec-WebSocket-Key")
	if key == "" {
		return nil, errors.New("missing Sec-WebSocket-Key")
	}

	// http.ResponseController is the modern route to the underlying connection;
	// it works through the wrappers middleware tends to add, where a direct
	// type assertion to http.Hijacker does not.
	conn, rw, err := http.NewResponseController(w).Hijack()
	if err != nil {
		return nil, err
	}

	accept := acceptKey(key)
	_, err = fmt.Fprintf(rw, "HTTP/1.1 101 Switching Protocols\r\n"+
		"Upgrade: websocket\r\n"+
		"Connection: Upgrade\r\n"+
		"Sec-WebSocket-Accept: %s\r\n\r\n", accept)
	if err == nil {
		err = rw.Flush()
	}
	if err != nil {
		conn.Close()
		return nil, err
	}
	return &liveClient{conn: conn, rw: rw}, nil
}

// acceptKey computes the Sec-WebSocket-Accept response value: the client's key
// concatenated with the protocol's magic GUID, SHA-1 hashed, base64 encoded.
//
// SHA-1 is not a security choice here and its weakness does not matter: the
// handshake proves only that the peer speaks WebSocket rather than having been
// tricked into sending a WebSocket-shaped request. The RFC specifies SHA-1, so
// SHA-1 it is.
func acceptKey(key string) string {
	sum := sha1.Sum([]byte(key + websocketGUID))
	return base64.StdEncoding.EncodeToString(sum[:])
}

// headerContainsToken reports whether a comma-separated header lists a token.
func headerContainsToken(header, token string) bool {
	for _, part := range strings.Split(header, ",") {
		if strings.EqualFold(strings.TrimSpace(part), token) {
			return true
		}
	}
	return false
}

// writeFrame writes one unfragmented frame.
//
// Server-to-client frames must not be masked (RFC 6455 section 5.1), which is
// the opposite of the rule for clients and a common source of confusion. The
// payload length is encoded in one of three widths depending on size.
func (c *liveClient) writeFrame(opcode byte, payload []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second)); err != nil {
		return err
	}

	var header [10]byte
	header[0] = 0x80 | opcode // FIN set, one frame
	n := 2
	switch size := len(payload); {
	case size < 126:
		header[1] = byte(size)
	case size < 1<<16:
		header[1] = 126
		binary.BigEndian.PutUint16(header[2:], uint16(size))
		n = 4
	default:
		header[1] = 127
		binary.BigEndian.PutUint64(header[2:], uint64(size))
		n = 10
	}

	if _, err := c.rw.Write(header[:n]); err != nil {
		return err
	}
	if _, err := c.rw.Write(payload); err != nil {
		return err
	}
	return c.rw.Flush()
}

// readLoop drains incoming frames, answering pings and honouring close.
func (c *liveClient) readLoop() {
	for {
		// No read deadline: an idle browser is the normal state, and the
		// connection should survive it. A vanished browser surfaces as a read
		// error when the socket closes.
		opcode, payload, err := c.readFrame()
		if err != nil {
			return
		}
		switch opcode {
		case opClose:
			_ = c.writeFrame(opClose, payload)
			return
		case opPing:
			if err := c.writeFrame(opPong, payload); err != nil {
				return
			}
		}
	}
}

// readFrame reads one frame, unmasking its payload.
//
// Client-to-server frames are always masked, and the mask must be applied
// before the payload means anything.
func (c *liveClient) readFrame() (opcode byte, payload []byte, err error) {
	var header [2]byte
	if _, err = io.ReadFull(c.rw, header[:]); err != nil {
		return 0, nil, err
	}
	opcode = header[0] & 0x0F
	masked := header[1]&0x80 != 0
	size := uint64(header[1] & 0x7F)

	switch size {
	case 126:
		var ext [2]byte
		if _, err = io.ReadFull(c.rw, ext[:]); err != nil {
			return 0, nil, err
		}
		size = uint64(binary.BigEndian.Uint16(ext[:]))
	case 127:
		var ext [8]byte
		if _, err = io.ReadFull(c.rw, ext[:]); err != nil {
			return 0, nil, err
		}
		size = binary.BigEndian.Uint64(ext[:])
	}

	// A browser has no reason to send bindery anything large. Refusing an
	// oversized frame keeps a hostile or broken client from asking for an
	// arbitrary allocation.
	const maxFrame = 1 << 20
	if size > maxFrame {
		return 0, nil, errors.New("websocket frame too large")
	}

	var mask [4]byte
	if masked {
		if _, err = io.ReadFull(c.rw, mask[:]); err != nil {
			return 0, nil, err
		}
	}
	payload = make([]byte, size)
	if _, err = io.ReadFull(c.rw, payload); err != nil {
		return 0, nil, err
	}
	if masked {
		for i := range payload {
			payload[i] ^= mask[i%4]
		}
	}
	return opcode, payload, nil
}
