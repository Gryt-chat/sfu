// Package audiotransport is the own voice transport's SFU side (GRYT-1818): WebTransport datagrams
// over QUIC, fanned out per room. Video rides the same sessions on a hub of its own once EnableVideo is called.
package audiotransport

import (
	"context"
	"crypto/tls"
	"fmt"
	"log"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/quic-go/quic-go/http3"
)

// The HTTP/3 settings a browser checks before it opens a WebTransport session (draft-ietf-webtrans-http3).
const (
	settingEnableWebTransportDraft06 = 0x2b603742
	settingWebTransportEnabled       = 0x2c7cf000
	settingWebTransportMaxSessions   = 0x14e9cd29
)

// Offer is what a client needs to open its audio session, sent over the signalling WebSocket.
type Offer struct {
	URLs     []string `json:"urls"`
	CertHash string   `json:"certHash"`
	Ticket   string   `json:"ticket"`
	// The native engine's plain UDP, when the listener runs (GRYT-1822).
	UDP *UDPOffer `json:"udp,omitempty"`
	// Video's own session, when it's turned on. It shares the cert, not the ticket or the UDP key.
	Video *VideoOffer `json:"video,omitempty"`
}

// VideoOffer is a joined peer's way into the video hub: WebTransport at /video, or a UDP session of its own.
type VideoOffer struct {
	URLs   []string  `json:"urls"`
	Ticket string    `json:"ticket"`
	UDP    *UDPOffer `json:"udp,omitempty"`
}

type Server struct {
	port    int
	hosts   []string
	certs   *certStore
	tickets *tickets
	hub     *hub
	h3      *http3.Server
	// video and its tickets stay nil until EnableVideo.
	video        *hub
	videoTickets *tickets

	mu  sync.Mutex
	udp *udpListener
}

// New listens on UDP port and tells clients to reach it at each of hosts (the SFU's public addresses).
func New(port int, hosts []string) *Server {
	var ips []net.IP
	for _, h := range hosts {
		if ip := net.ParseIP(h); ip != nil {
			ips = append(ips, ip)
		}
	}
	s := &Server{port: port, hosts: hosts, certs: newCertStore(ips), tickets: newTickets(), hub: newHub()}
	mux := http.NewServeMux()
	mux.HandleFunc("/audio", func(w http.ResponseWriter, r *http.Request) { s.handle(w, r, s.hub, s.tickets) })
	mux.HandleFunc("/video", func(w http.ResponseWriter, r *http.Request) {
		if s.video == nil {
			http.NotFound(w, r)
			return
		}
		s.handle(w, r, s.video, s.videoTickets)
	})
	s.h3 = &http3.Server{
		Addr:    ":" + strconv.Itoa(port),
		Handler: mux,
		TLSConfig: &tls.Config{
			MinVersion: tls.VersionTLS13,
			NextProtos: []string{http3.NextProtoH3},
			GetCertificate: func(*tls.ClientHelloInfo) (*tls.Certificate, error) {
				cert, _, err := s.certs.current(time.Now())
				return cert, err
			},
		},
		EnableDatagrams: true,
		AdditionalSettings: map[uint64]uint64{
			settingEnableWebTransportDraft06: 1,
			settingWebTransportEnabled:       1,
			settingWebTransportMaxSessions:   1,
		},
	}
	return s
}

// OnSenders calls fn with a room's sender ids, by peer, whenever someone's session joins or leaves
// it, so clients can tell whose packet is whose (GRYT-1830). Set it before serving.
func (s *Server) OnSenders(fn func(roomID string, senders map[string]uint32)) {
	s.hub.changed = func(roomID string) { fn(roomID, s.hub.senders(roomID)) }
}

// Senders is who sends on the transport in a room right now, by peer.
func (s *Server) Senders(roomID string) map[string]uint32 { return s.hub.senders(roomID) }

// EnableVideo opens the video hub. Call it before serving, and before ListenUDP.
func (s *Server) EnableVideo() {
	s.video = newVideoHub()
	s.videoTickets = newTickets()
}

// OnVideoSenders is OnSenders for the video hub, whose ids are its own. Does nothing while video is off.
func (s *Server) OnVideoSenders(fn func(roomID string, senders map[string]uint32)) {
	if s.video != nil {
		s.video.changed = func(roomID string) { fn(roomID, s.video.senders(roomID)) }
	}
}

// ListenAndServe blocks; run it in a goroutine.
func (s *Server) ListenAndServe() error { return s.h3.ListenAndServe() }

// Offer hands a joined peer a ticket for its audio session.
func (s *Server) Offer(peerID, roomID string) (Offer, error) {
	_, hash, err := s.certs.current(time.Now())
	if err != nil {
		return Offer{}, err
	}
	ticket, err := s.tickets.issue(peerID, roomID, time.Now())
	if err != nil {
		return Offer{}, err
	}
	urls := make([]string, 0, len(s.hosts))
	for _, h := range s.hosts {
		urls = append(urls, fmt.Sprintf("https://%s/audio", net.JoinHostPort(h, strconv.Itoa(s.port))))
	}
	offer := Offer{URLs: urls, CertHash: hash, Ticket: ticket}
	s.mu.Lock()
	u := s.udp
	s.mu.Unlock()
	if u != nil {
		if offer.UDP, err = u.offer(peerID, roomID, time.Now()); err != nil {
			return Offer{}, err
		}
	}
	if s.video != nil {
		if offer.Video, err = s.videoOffer(u, peerID, roomID); err != nil {
			return Offer{}, err
		}
	}
	return offer, nil
}

func (s *Server) videoOffer(u *udpListener, peerID, roomID string) (*VideoOffer, error) {
	ticket, err := s.videoTickets.issue(peerID, roomID, time.Now())
	if err != nil {
		return nil, err
	}
	o := &VideoOffer{Ticket: ticket}
	for _, h := range s.hosts {
		o.URLs = append(o.URLs, fmt.Sprintf("https://%s/video", net.JoinHostPort(h, strconv.Itoa(s.port))))
	}
	if u != nil {
		if o.UDP, err = u.offerVideo(peerID, roomID, time.Now()); err != nil {
			return nil, err
		}
	}
	return o, nil
}

// Drop ends a peer's audio and video sessions; called when it leaves voice.
func (s *Server) Drop(peerID string) {
	s.hub.drop(peerID)
	if s.video != nil {
		s.video.drop(peerID)
	}
	s.mu.Lock()
	u := s.udp
	s.mu.Unlock()
	if u != nil {
		u.forget(peerID)
	}
}

func isWebTransport(r *http.Request) bool {
	return r.Method == http.MethodConnect && (r.Proto == "webtransport-h3" || r.Proto == "webtransport")
}

func (s *Server) handle(w http.ResponseWriter, r *http.Request, h *hub, tk *tickets) {
	if !isWebTransport(r) {
		http.Error(w, "webtransport only", http.StatusBadRequest)
		return
	}
	t, ok := tk.redeem(r.URL.Query().Get("ticket"), time.Now())
	if !ok {
		http.Error(w, "no such ticket", http.StatusUnauthorized)
		return
	}
	settingser, ok := w.(http3.Settingser)
	if !ok {
		http.Error(w, "not http/3", http.StatusBadRequest)
		return
	}
	select {
	case <-settingser.ReceivedSettings():
	case <-time.After(5 * time.Second):
		http.Error(w, "no settings", http.StatusBadRequest)
		return
	}
	if !settingser.Settings().EnableDatagrams {
		http.Error(w, "datagrams required", http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusOK)
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
	str := w.(http3.HTTPStreamer).HTTPStream()
	ctx, cancel := context.WithCancel(r.Context())
	m := h.join(t.roomID, t.peerID, str, func() {
		cancel()
		str.CancelRead(0)
		_ = str.Close()
	})
	defer h.leave(m)
	defer cancel()
	for {
		b, err := str.ReceiveDatagram(ctx)
		if err != nil {
			if ctx.Err() == nil {
				log.Printf("[AUDIO] session %s ended: %v", t.peerID, err)
			}
			return
		}
		h.forward(m, b, time.Now())
	}
}
