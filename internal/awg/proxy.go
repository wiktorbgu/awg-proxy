package awg

import (
	"io"
	"net"
	"os"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

const bufSize = 1500 // standard MTU

// Proxy is a UDP proxy that transforms WireGuard packets to AmneziaWG format.
type Proxy struct {
	cfg        *Config
	listenAddr *net.UDPAddr
	remoteAddr *net.UDPAddr
	clientAddr atomic.Pointer[net.UDPAddr]
	pool       sync.Pool
	lastRecv   atomic.Int64 // unix timestamp of last packet from server
}

// NewProxy creates a new Proxy instance.
func NewProxy(cfg *Config, listenAddr, remoteAddr *net.UDPAddr) *Proxy {
	p := &Proxy{
		cfg:        cfg,
		listenAddr: listenAddr,
		remoteAddr: remoteAddr,
	}
	p.pool.New = func() any {
		b := make([]byte, bufSize)
		return &b
	}
	return p
}

// Run starts the proxy and blocks until stop is called or a fatal error occurs.
// The stop channel is closed to signal shutdown.
func (p *Proxy) Run(stop <-chan struct{}) error {
	listenConn, err := net.ListenUDP("udp", p.listenAddr)
	if err != nil {
		return err
	}
	defer listenConn.Close()

	remoteConn, err := net.DialUDP("udp", nil, p.remoteAddr)
	if err != nil {
		return err
	}

	p.lastRecv.Store(time.Now().Unix())

	var wg sync.WaitGroup
	wg.Add(2)

	// Channel to pass new remote connections to the serverToClient goroutine.
	remoteCh := make(chan *net.UDPConn, 1)

	go func() {
		defer wg.Done()
		p.clientToServer(listenConn, remoteConn, remoteCh, stop)
	}()

	go func() {
		defer wg.Done()
		p.serverToClient(listenConn, remoteConn, remoteCh, stop)
	}()

	wg.Wait()
	remoteConn.Close()
	return nil
}

func (p *Proxy) clientToServer(listenConn *net.UDPConn, remoteConn *net.UDPConn, remoteCh <-chan *net.UDPConn, stop <-chan struct{}) {
	currentRemote := remoteConn

	for {
		// Check for new remote connection (non-blocking).
		select {
		case newRemote := <-remoteCh:
			currentRemote = newRemote
		default:
		}

		select {
		case <-stop:
			return
		default:
		}

		bufp := p.pool.Get().(*[]byte)
		buf := *bufp

		listenConn.SetReadDeadline(time.Now().Add(1 * time.Second))
		n, addr, err := listenConn.ReadFromUDP(buf)
		if err != nil {
			p.pool.Put(bufp)
			if isTimeout(err) {
				continue
			}
			if isClosedErr(err) {
				return
			}
			LogError(p.cfg, "listen read: ", err.Error())
			continue
		}

		// Update client address.
		if cur := p.clientAddr.Load(); cur == nil || !addrEqual(cur, addr) {
			p.clientAddr.Store(addr)
			LogInfo(p.cfg, "client: ", addr.String())
		}

		out, sendJunk := TransformOutbound(buf, n, p.cfg)

		LogDebug(p.cfg, "c->s: recv ", strconv.Itoa(n), "B, send ", strconv.Itoa(len(out)), "B, junk=", strconv.FormatBool(sendJunk))

		if sendJunk {
			junkPackets := GenerateJunkPackets(p.cfg)
			for i, junk := range junkPackets {
				if _, err := currentRemote.Write(junk); err != nil {
					LogDebug(p.cfg, "c->s: junk ", strconv.Itoa(i), " write err: ", err.Error())
					break // connection likely closed during reconnect
				}
				LogDebug(p.cfg, "c->s: junk ", strconv.Itoa(i+1), "/", strconv.Itoa(len(junkPackets)), " ", strconv.Itoa(len(junk)), "B sent")
			}
		}

		_, err = currentRemote.Write(out)
		p.pool.Put(bufp)
		if err != nil {
			if isClosedErr(err) {
				continue // reconnect in progress, WG will retransmit
			}
			LogError(p.cfg, "remote write: ", err.Error())
		} else {
			LogDebug(p.cfg, "c->s: transformed ", strconv.Itoa(len(out)), "B sent to server")
		}
	}
}

func (p *Proxy) serverToClient(listenConn *net.UDPConn, remoteConn *net.UDPConn, remoteCh chan<- *net.UDPConn, stop <-chan struct{}) {
	currentRemote := remoteConn
	timeout := time.Duration(p.cfg.Timeout) * time.Second
	if timeout <= 0 {
		timeout = 180 * time.Second
	}

	backoff := time.Second

	for {
		select {
		case <-stop:
			return
		default:
		}

		bufp := p.pool.Get().(*[]byte)
		buf := *bufp

		currentRemote.SetReadDeadline(time.Now().Add(5 * time.Second))
		n, err := currentRemote.Read(buf)
		if err != nil {
			p.pool.Put(bufp)
			if isTimeout(err) {
				// Check inactivity timeout.
				if time.Since(time.Unix(p.lastRecv.Load(), 0)) > timeout {
					LogInfo(p.cfg, "remote timeout, reconnecting")
					newConn := p.reconnectRemote(stop, &backoff)
					if newConn == nil {
						return // shutdown
					}
					currentRemote.Close()
					currentRemote = newConn
					// Notify clientToServer of new connection.
					select {
					case remoteCh <- newConn:
					default:
					}
					p.clientAddr.Store(nil) // reset client
				}
				continue
			}
			if isClosedErr(err) {
				return
			}
			LogError(p.cfg, "remote read: ", err.Error())
			// Try reconnect on read error.
			newConn := p.reconnectRemote(stop, &backoff)
			if newConn == nil {
				return
			}
			currentRemote.Close()
			currentRemote = newConn
			select {
			case remoteCh <- newConn:
			default:
			}
			continue
		}

		p.lastRecv.Store(time.Now().Unix())
		backoff = time.Second // reset backoff on success

		LogDebug(p.cfg, "s->c: recv ", strconv.Itoa(n), "B from server")

		out, valid := TransformInbound(buf, n, p.cfg)
		if !valid {
			LogDebug(p.cfg, "s->c: invalid/junk packet ", strconv.Itoa(n), "B, dropped")
			p.pool.Put(bufp)
			continue
		}

		LogDebug(p.cfg, "s->c: transformed ", strconv.Itoa(len(out)), "B, valid=true")

		clientAddr := p.clientAddr.Load()
		if clientAddr != nil {
			_, err = listenConn.WriteToUDP(out, clientAddr)
			if err != nil {
				LogError(p.cfg, "listen write: ", err.Error())
			} else {
				LogDebug(p.cfg, "s->c: sent ", strconv.Itoa(len(out)), "B to ", clientAddr.String())
			}
		} else {
			LogDebug(p.cfg, "s->c: no client addr, packet dropped")
		}
		p.pool.Put(bufp)
	}
}

// reconnectRemote attempts to reconnect to the remote AWG server with exponential backoff.
func (p *Proxy) reconnectRemote(stop <-chan struct{}, backoff *time.Duration) *net.UDPConn {
	const maxBackoff = 30 * time.Second

	for {
		select {
		case <-stop:
			return nil
		default:
		}

		LogInfo(p.cfg, "reconnecting to ", p.remoteAddr.String())

		// Re-resolve the address (handles DNS changes).
		addr, err := net.ResolveUDPAddr("udp", p.remoteAddr.String())
		if err != nil {
			LogError(p.cfg, "resolve: ", err.Error())
		} else {
			conn, err := net.DialUDP("udp", nil, addr)
			if err == nil {
				LogInfo(p.cfg, "reconnected to ", addr.String())
				p.lastRecv.Store(time.Now().Unix())
				*backoff = time.Second
				return conn
			}
			LogError(p.cfg, "dial: ", err.Error())
		}

		// Wait with backoff.
		timer := time.NewTimer(*backoff)
		select {
		case <-stop:
			timer.Stop()
			return nil
		case <-timer.C:
		}

		*backoff *= 2
		if *backoff > maxBackoff {
			*backoff = maxBackoff
		}
	}
}

func addrEqual(a, b *net.UDPAddr) bool {
	return a.Port == b.Port && a.IP.Equal(b.IP)
}

func isTimeout(err error) bool {
	ne, ok := err.(net.Error)
	return ok && ne.Timeout()
}

func isClosedErr(err error) bool {
	// net.ErrClosed is returned when reading from a closed connection.
	return err == net.ErrClosed || isClosedErrString(err)
}

func isClosedErrString(err error) bool {
	s := err.Error()
	for i := 0; i+len("use of closed") <= len(s); i++ {
		if s[i:i+len("use of closed")] == "use of closed" {
			return true
		}
	}
	return false
}

// Logging helpers — write directly to stdout, no fmt/log dependency.

func LogInfo(cfg *Config, parts ...string) {
	if cfg.LogLevel < LevelInfo {
		return
	}
	writeLog("INFO: ", parts)
}

func LogError(cfg *Config, parts ...string) {
	if cfg.LogLevel < LevelError {
		return
	}
	writeLog("ERROR: ", parts)
}

func LogDebug(cfg *Config, parts ...string) {
	if cfg.LogLevel < LevelDebug {
		return
	}
	writeLog("DEBUG: ", parts)
}

func writeLog(prefix string, parts []string) {
	io.WriteString(os.Stderr, prefix)
	for _, s := range parts {
		io.WriteString(os.Stderr, s)
	}
	io.WriteString(os.Stderr, "\n")
}
