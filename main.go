package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/emiago/sipgo"
	"github.com/emiago/sipgo/sip"
	"github.com/fsnotify/fsnotify"
	"github.com/google/uuid"
	"github.com/pion/sdp/v3"
)

type TenantConfig struct {
	Whitelist      []string `json:"whitelist"`
	BackendServers []string `json:"backend_servers"`
}

type Config struct {
	ExternalIP string                  `json:"external_ip"`
	InternalIP string                  `json:"internal_ip"`
	Ports      []int                   `json:"ports"`
	Tenants    map[string]TenantConfig `json:"tenants"`
}

var (
	config       Config
	configMutex  sync.RWMutex
	activeCalls  sync.Map
	keepAliveInt = 30 * time.Second
)

func loadConfig(filename string) error {
	file, err := os.Open(filename)
	if err != nil {
		return fmt.Errorf("failed to open config file: %w", err)
	}
	defer file.Close()

	decoder := json.NewDecoder(file)
	err = decoder.Decode(&config)
	if err != nil {
		return fmt.Errorf("failed to decode config: %w", err)
	}
	return nil
}

func watchConfigFile(filename string) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		log.Fatalf("Failed to create file watcher: %v", err)
	}
	defer watcher.Close()

	go func() {
		for {
			select {
			case event, ok := <-watcher.Events:
				if !ok {
					return
				}
				if event.Op&fsnotify.Write == fsnotify.Write {
					log.Println("Configuration file modified:", event.Name)
					if err := loadConfig(filename); err != nil {
						log.Printf("Failed to reload config: %v", err)
					} else {
						log.Printf("Configuration reloaded successfully")
					}
				}
			case err, ok := <-watcher.Errors:
				if !ok {
					return
				}
				log.Println("Error watching config file:", err)
			}
		}
	}()

	err = watcher.Add(filename)
	if err != nil {
		log.Fatalf("Failed to add file to watcher: %v", err)
	}
}

func isWhitelisted(ip string, whitelist []string) bool {
	for _, whitelistedIP := range whitelist {
		if ip == whitelistedIP {
			return true
		}
	}
	return false
}

func getCallUUID(req *sip.Request) string {
	callIDHeader := req.CallID()
	if callIDHeader == nil || callIDHeader.String() == "" {
		return uuid.New().String()
	}
	return callIDHeader.String()
}

func getTenantFromRequest(req *sip.Request) string {
	fromHeader := req.From()
	if fromHeader != nil {
		addr := fromHeader.Address.String()
		parts := strings.Split(addr, "@")
		if len(parts) > 1 {
			return parts[1]
		}
	}
	return ""
}

func handleSiprecInvite(req *sip.Request, tx sip.ServerTransaction, tenantConfig TenantConfig, externalIP, internalIP string, ua *sipgo.UserAgent) {
	callUUID := getCallUUID(req)
	log.Printf("Handling SIPREC INVITE for call UUID: %s", callUUID)

	sdpBody := req.Body()
	sdpParsed := &sdp.SessionDescription{}
	err := sdpParsed.Unmarshal(sdpBody)
	if err != nil {
		log.Printf("Failed to parse SDP: %v", err)
		resp := sip.NewResponseFromRequest(req, 400, "Bad Request", nil)
		tx.Respond(resp)
		return
	}

	remoteAddr, _, _ := net.SplitHostPort(req.Source())
	ipToUse := internalIP
	if isWhitelisted(remoteAddr, tenantConfig.Whitelist) {
		ipToUse = externalIP
	}

	// Generate SDP response based on the received SDP and the appropriate IP
	sdpResponse := generateSDPResponse(sdpParsed, ipToUse)

	sdpResponseBytes, err := sdpResponse.Marshal()
	if err != nil {
		log.Printf("Failed to marshal SDP response: %v", err)
		resp := sip.NewResponseFromRequest(req, 500, "Internal Server Error", nil)
		tx.Respond(resp)
		return
	}

	// Create a 200 OK response with SDP
	resp := sip.NewResponseFromRequest(req, 200, "OK", nil)
	resp.SetBody(sdpResponseBytes)

	tx.Respond(resp)
	log.Printf("Sent 200 OK response to %s for call UUID: %s", req.Source(), callUUID)

	// Start RTP forwarding
	rtpForwarders := startRTPForwarding(sdpResponse.MediaDescriptions, tenantConfig.BackendServers)

	// Store call information
	activeCalls.Store(callUUID, rtpForwarders)

	// Start keep-alive mechanism
	go keepAlive(ua, req, callUUID)
}

func generateSDPResponse(receivedSDP *sdp.SessionDescription, ipToUse string) *sdp.SessionDescription {
	mediaStreams := make([]*sdp.MediaDescription, len(receivedSDP.MediaDescriptions))

	for i, media := range receivedSDP.MediaDescriptions {
		newMedia := &sdp.MediaDescription{
			MediaName: sdp.MediaName{
				Media:   media.MediaName.Media,
				Port:    media.MediaName.Port,
				Protos:  media.MediaName.Protos,
				Formats: media.MediaName.Formats,
			},
			ConnectionInformation: &sdp.ConnectionInformation{
				NetworkType: "IN",
				AddressType: "IP4",
				Address:     &sdp.Address{Address: ipToUse},
			},
			Attributes: make([]sdp.Attribute, 0, len(media.Attributes)),
		}

		// Copy relevant attributes
		for _, attr := range media.Attributes {
			if attr.Key == "rtpmap" || attr.Key == "fmtp" {
				newMedia.Attributes = append(newMedia.Attributes, attr)
			}
		}

		mediaStreams[i] = newMedia
	}

	return &sdp.SessionDescription{
		Origin: sdp.Origin{
			Username:       "-",
			SessionID:      receivedSDP.Origin.SessionID,
			SessionVersion: receivedSDP.Origin.SessionVersion + 1,
			NetworkType:    "IN",
			AddressType:    "IP4",
			UnicastAddress: ipToUse,
		},
		SessionName:           receivedSDP.SessionName,
		ConnectionInformation: &sdp.ConnectionInformation{NetworkType: "IN", AddressType: "IP4", Address: &sdp.Address{Address: ipToUse}},
		TimeDescriptions:      receivedSDP.TimeDescriptions,
		MediaDescriptions:     mediaStreams,
	}
}

func startRTPForwarding(mediaStreams []*sdp.MediaDescription, backendServers []string) []*RTPForwarder {
	forwarders := make([]*RTPForwarder, len(mediaStreams))

	for i, media := range mediaStreams {
		forwarder := NewRTPForwarder(media.MediaName.Port.Value, backendServers)
		go forwarder.Start()
		forwarders[i] = forwarder
	}

	return forwarders
}

type RTPForwarder struct {
	localPort      int
	backendServers []string
	conn           net.PacketConn
	backendConns   []net.Conn
	stopChan       chan struct{}
}

func NewRTPForwarder(localPort int, backendServers []string) *RTPForwarder {
	return &RTPForwarder{
		localPort:      localPort,
		backendServers: backendServers,
		stopChan:       make(chan struct{}),
	}
}

func (f *RTPForwarder) Start() {
	var err error
	f.conn, err = net.ListenPacket("udp", fmt.Sprintf(":%d", f.localPort))
	if err != nil {
		log.Printf("Failed to listen on port %d: %v", f.localPort, err)
		return
	}
	defer f.conn.Close()

	f.backendConns = make([]net.Conn, len(f.backendServers))
	for i, backend := range f.backendServers {
		backendConn, err := net.Dial("udp", backend)
		if err != nil {
			log.Printf("Failed to connect to backend server %s: %v", backend, err)
			continue
		}
		f.backendConns[i] = backendConn
		defer backendConn.Close()
	}

	packet := make([]byte, 1600)
	for {
		select {
		case <-f.stopChan:
			return
		default:
			n, _, err := f.conn.ReadFrom(packet)
			if err != nil {
				log.Printf("Failed to read RTP packet: %v", err)
				continue
			}

			for _, backendConn := range f.backendConns {
				if backendConn != nil {
					_, err := backendConn.Write(packet[:n])
					if err != nil {
						log.Printf("Failed to forward RTP packet to backend server: %v", err)
					}
				}
			}
		}
	}
}

func (f *RTPForwarder) Stop() {
	close(f.stopChan)
	if f.conn != nil {
		f.conn.Close()
	}
	for _, conn := range f.backendConns {
		if conn != nil {
			conn.Close()
		}
	}
}

func keepAlive(ua *sipgo.UserAgent, req *sip.Request, callUUID string) {
	ticker := time.NewTicker(keepAliveInt)
	defer ticker.Stop()

	client, err := sipgo.NewClient(ua)
	if err != nil {
		log.Printf("Failed to create SIP client for keep-alive: %v", err)
		return
	}

	for range ticker.C {
		options := sip.NewRequest(sip.OPTIONS, req.Recipient)
		options.SetDestination(req.Source())
		callID := sip.CallIDHeader(callUUID)
		options.AppendHeader(&callID)

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		// Use the Client's Do method to send the request and receive the response
		resp, err := client.Do(ctx, options)
		if err != nil {
			log.Printf("Failed to send OPTIONS keep-alive for call %s: %v", callUUID, err)
			continue
		}

		if resp.StatusCode >= 300 {
			log.Printf("Received non-2xx response to OPTIONS keep-alive for call %s: %v", callUUID, resp)
			cleanupCall(callUUID)
			return
		}

		log.Printf("Received successful response to OPTIONS keep-alive for call %s", callUUID)
	}
}

func cleanupCall(callUUID string) {
	if forwarders, ok := activeCalls.Load(callUUID); ok {
		for _, forwarder := range forwarders.([]*RTPForwarder) {
			forwarder.Stop()
		}
		activeCalls.Delete(callUUID)
		log.Printf("Cleaned up call %s", callUUID)
	}
}

func handleBye(req *sip.Request, tx sip.ServerTransaction) {
	callUUID := getCallUUID(req)
	log.Printf("Received BYE for call UUID: %s", callUUID)

	cleanupCall(callUUID)

	resp := sip.NewResponseFromRequest(req, 200, "OK", nil)
	tx.Respond(resp)
}

func startServer(wg *sync.WaitGroup) {
	defer wg.Done()

	ip := "0.0.0.0" // Listen on all interfaces

	ua, err := sipgo.NewUA()
	if err != nil {
		log.Fatalf("Failed to create UserAgent: %v", err)
	}

	server, err := sipgo.NewServer(ua)
	if err != nil {
		log.Fatalf("Failed to create SIP server: %v", err)
	}

	server.OnRequest(sip.INVITE, func(req *sip.Request, tx sip.ServerTransaction) {
		tenant := getTenantFromRequest(req)
		if tenant == "" {
			log.Printf("Failed to determine tenant from request")
			resp := sip.NewResponseFromRequest(req, 400, "Bad Request", nil)
			tx.Respond(resp)
			return
		}

		configMutex.RLock()
		tenantConfig, tenantExists := config.Tenants[tenant]
		externalIP := config.ExternalIP
		internalIP := config.InternalIP
		configMutex.RUnlock()

		if !tenantExists {
			log.Printf("Tenant not found: %s", tenant)
			resp := sip.NewResponseFromRequest(req, 404, "Not Found", nil)
			tx.Respond(resp)
			return
		}

		remoteAddr, _, _ := net.SplitHostPort(req.Source())
		if !isWhitelisted(remoteAddr, tenantConfig.Whitelist) {
			log.Printf("Rejected request from non-whitelisted IP: %s for tenant: %s", remoteAddr, tenant)
			resp := sip.NewResponseFromRequest(req, 403, "Forbidden", nil)
			tx.Respond(resp)
			return
		}

		log.Printf("Received SIPREC INVITE from %s for tenant: %s", remoteAddr, tenant)
		handleSiprecInvite(req, tx, tenantConfig, externalIP, internalIP, ua)
	})

	server.OnRequest(sip.BYE, handleBye)

	for _, port := range config.Ports {
		address := fmt.Sprintf("%s:%d", ip, port)
		go func(port int) {
			if err := server.ListenAndServe(context.Background(), "udp", address); err != nil {
				log.Fatalf("Failed to start SIP server on port %d: %v", port, err)
			}
			if err := server.ListenAndServe(context.Background(), "tcp", address); err != nil {
				log.Fatalf("Failed to start SIP server on port %d: %v", port, err)
			}
			log.Printf("SIP server started on %s", address)
		}(port)
	}
}

func main() {
	if err := loadConfig("config.json"); err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	var wg sync.WaitGroup
	wg.Add(1)
	go startServer(&wg)

	go watchConfigFile("config.json")

	wg.Wait()
}
