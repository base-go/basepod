// Package dns provides a built-in DNS server for local development.
// It resolves *.domain queries to the server IP and forwards others upstream.
package dns

import (
	"fmt"
	"log"
	"net"
	"strings"
	"sync"
)

// Server is a simple DNS server for local development
type Server struct {
	domain    string   // e.g., "base.pod"
	serverIP  net.IP   // IP to resolve domain queries to
	upstream  []string // upstream DNS servers
	port      int
	listener  net.PacketConn
	tcpListener net.Listener
	running   bool
	mu        sync.RWMutex
}

// Config holds DNS server configuration
type Config struct {
	Domain   string   // Domain suffix to handle (e.g., "base.pod")
	ServerIP string   // IP address to return for domain queries
	Port     int      // Port to listen on (default 53)
	Upstream []string // Upstream DNS servers (default: 8.8.8.8, 1.1.1.1)
}

// NewServer creates a new DNS server
func NewServer(cfg Config) (*Server, error) {
	if cfg.Domain == "" {
		return nil, fmt.Errorf("domain is required")
	}

	ip := net.ParseIP(cfg.ServerIP)
	if ip == nil {
		// Try to detect server IP
		ip = detectLocalIP()
	}
	if ip == nil {
		return nil, fmt.Errorf("could not determine server IP")
	}

	port := cfg.Port
	if port == 0 {
		port = 53
	}

	upstream := cfg.Upstream
	if len(upstream) == 0 {
		upstream = []string{"8.8.8.8:53", "1.1.1.1:53"}
	}

	return &Server{
		domain:   strings.TrimPrefix(cfg.Domain, "."),
		serverIP: ip.To4(),
		upstream: upstream,
		port:     port,
	}, nil
}

// Start starts the DNS server
func (s *Server) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.running {
		return fmt.Errorf("server already running")
	}

	// Start UDP listener
	udpAddr := fmt.Sprintf(":%d", s.port)
	udpConn, err := net.ListenPacket("udp", udpAddr)
	if err != nil {
		return fmt.Errorf("failed to listen on UDP %s: %w", udpAddr, err)
	}
	s.listener = udpConn

	// Start TCP listener
	tcpAddr := fmt.Sprintf(":%d", s.port)
	tcpListener, err := net.Listen("tcp", tcpAddr)
	if err != nil {
		udpConn.Close()
		return fmt.Errorf("failed to listen on TCP %s: %w", tcpAddr, err)
	}
	s.tcpListener = tcpListener

	s.running = true

	// Handle UDP queries
	go s.serveUDP()

	// Handle TCP queries
	go s.serveTCP()

	log.Printf("DNS server started on port %d (resolving *.%s -> %s)", s.port, s.domain, s.serverIP)
	return nil
}

// Stop stops the DNS server
func (s *Server) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.running {
		return nil
	}

	s.running = false

	if s.listener != nil {
		s.listener.Close()
	}
	if s.tcpListener != nil {
		s.tcpListener.Close()
	}

	return nil
}

func (s *Server) serveUDP() {
	buf := make([]byte, 512)
	for {
		s.mu.RLock()
		running := s.running
		s.mu.RUnlock()
		if !running {
			return
		}

		n, addr, err := s.listener.ReadFrom(buf)
		if err != nil {
			if s.running {
				log.Printf("DNS UDP read error: %v", err)
			}
			continue
		}

		go s.handleQuery(buf[:n], addr)
	}
}

func (s *Server) serveTCP() {
	for {
		s.mu.RLock()
		running := s.running
		s.mu.RUnlock()
		if !running {
			return
		}

		conn, err := s.tcpListener.Accept()
		if err != nil {
			if s.running {
				log.Printf("DNS TCP accept error: %v", err)
			}
			continue
		}

		go s.handleTCPQuery(conn)
	}
}

func (s *Server) handleQuery(query []byte, addr net.Addr) {
	response := s.processQuery(query)
	if response != nil {
		s.listener.WriteTo(response, addr)
	}
}

func (s *Server) handleTCPQuery(conn net.Conn) {
	defer conn.Close()

	// Read length prefix (2 bytes)
	lenBuf := make([]byte, 2)
	if _, err := conn.Read(lenBuf); err != nil {
		return
	}
	length := int(lenBuf[0])<<8 | int(lenBuf[1])

	// Read query
	query := make([]byte, length)
	if _, err := conn.Read(query); err != nil {
		return
	}

	response := s.processQuery(query)
	if response != nil {
		// Write length prefix
		respLen := len(response)
		conn.Write([]byte{byte(respLen >> 8), byte(respLen)})
		conn.Write(response)
	}
}

func (s *Server) processQuery(query []byte) []byte {
	if len(query) < 12 {
		return nil
	}

	// Parse DNS header
	id := uint16(query[0])<<8 | uint16(query[1])
	qdcount := int(query[4])<<8 | int(query[5])

	if qdcount == 0 {
		return nil
	}

	// Parse question
	name, offset := parseDomainName(query, 12)
	if offset+4 > len(query) {
		return nil
	}

	qtype := uint16(query[offset])<<8 | uint16(query[offset+1])
	// qclass := uint16(query[offset+2])<<8 | uint16(query[offset+3])

	// Check if this is our domain
	nameLower := strings.ToLower(name)
	if s.matchesDomain(nameLower) && qtype == 1 { // A record
		return s.buildResponse(id, query[:offset+4], name, s.serverIP)
	}

	// Forward to upstream
	return s.forwardQuery(query)
}

func (s *Server) matchesDomain(name string) bool {
	name = strings.TrimSuffix(name, ".")
	domain := s.domain

	// Exact match or subdomain match
	return name == domain || strings.HasSuffix(name, "."+domain)
}

func (s *Server) buildResponse(id uint16, question []byte, name string, ip net.IP) []byte {
	response := make([]byte, 0, 512)

	// Header
	response = append(response,
		byte(id>>8), byte(id), // ID
		0x81, 0x80, // Flags: response, recursion available
		0x00, 0x01, // QDCOUNT: 1
		0x00, 0x01, // ANCOUNT: 1
		0x00, 0x00, // NSCOUNT: 0
		0x00, 0x00, // ARCOUNT: 0
	)

	// Question (copy from query)
	response = append(response, question[12:]...)

	// Answer
	// Name pointer to question
	response = append(response, 0xc0, 0x0c)
	// Type A
	response = append(response, 0x00, 0x01)
	// Class IN
	response = append(response, 0x00, 0x01)
	// TTL (300 seconds)
	response = append(response, 0x00, 0x00, 0x01, 0x2c)
	// RDLENGTH (4 for IPv4)
	response = append(response, 0x00, 0x04)
	// RDATA (IP address)
	response = append(response, ip[0], ip[1], ip[2], ip[3])

	return response
}

func (s *Server) forwardQuery(query []byte) []byte {
	for _, upstream := range s.upstream {
		conn, err := net.Dial("udp", upstream)
		if err != nil {
			continue
		}
		defer conn.Close()

		conn.Write(query)

		response := make([]byte, 512)
		n, err := conn.Read(response)
		if err != nil {
			continue
		}

		return response[:n]
	}
	return nil
}

func parseDomainName(data []byte, offset int) (string, int) {
	var name strings.Builder
	for {
		if offset >= len(data) {
			break
		}
		length := int(data[offset])
		if length == 0 {
			offset++
			break
		}
		if length&0xc0 == 0xc0 {
			// Pointer
			offset += 2
			break
		}
		offset++
		if offset+length > len(data) {
			break
		}
		if name.Len() > 0 {
			name.WriteByte('.')
		}
		name.Write(data[offset : offset+length])
		offset += length
	}
	return name.String(), offset
}

func detectLocalIP() net.IP {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return nil
	}

	for _, addr := range addrs {
		if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			if ip4 := ipnet.IP.To4(); ip4 != nil {
				// Prefer private IPs
				if ip4[0] == 192 || ip4[0] == 10 || (ip4[0] == 172 && ip4[1] >= 16 && ip4[1] <= 31) {
					return ip4
				}
			}
		}
	}

	// Fallback to any non-loopback IPv4
	for _, addr := range addrs {
		if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			if ip4 := ipnet.IP.To4(); ip4 != nil {
				return ip4
			}
		}
	}

	return nil
}
