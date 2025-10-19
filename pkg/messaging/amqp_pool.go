package messaging

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"math/rand"
	"net"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/streadway/amqp"
	"siprec-server/pkg/config"
)

// AMQPPool manages a pool of AMQP connections with load balancing
type AMQPPool struct {
	logger        *logrus.Logger
	config        *config.AMQPConfig
	connections   []*PooledConnection
	connMutex     sync.RWMutex
	roundRobin    uint64
	shutdownChan  chan struct{}
	shutdownOnce  sync.Once
	healthChecker *HealthChecker
	metrics       *AMQPMetrics
}

// PooledConnection represents a connection in the pool
type PooledConnection struct {
	conn      *amqp.Connection
	channels  chan *PooledChannel
	host      string
	healthy   bool
	lastUsed  time.Time
	created   time.Time
	mutex     sync.RWMutex
	connIndex int
}

// PooledChannel represents a channel in the pool
type PooledChannel struct {
	channel     *amqp.Channel
	inUse       bool
	lastUsed    time.Time
	confirmMode bool
	mutex       sync.Mutex
}

// HealthChecker monitors connection health
type HealthChecker struct {
	pool     *AMQPPool
	interval time.Duration
	stopChan chan struct{}
}

// AMQPMetrics holds pool metrics
type AMQPMetrics struct {
	TotalConnections  int64
	ActiveConnections int64
	TotalChannels     int64
	ActiveChannels    int64
	PublishedMessages int64
	FailedPublishes   int64
	ReconnectAttempts int64
	mutex             sync.RWMutex
}

// NewAMQPPool creates a new AMQP connection pool
func NewAMQPPool(logger *logrus.Logger, config *config.AMQPConfig) *AMQPPool {
	pool := &AMQPPool{
		logger:       logger,
		config:       config,
		connections:  make([]*PooledConnection, 0),
		shutdownChan: make(chan struct{}),
		metrics:      &AMQPMetrics{},
	}

	// Initialize health checker if enabled
	if config.LoadBalancing.HealthCheck {
		pool.healthChecker = &HealthChecker{
			pool:     pool,
			interval: config.MetricsInterval,
			stopChan: make(chan struct{}),
		}
	}

	return pool
}

// Initialize creates the initial connection pool
func (p *AMQPPool) Initialize() error {
	p.connMutex.Lock()
	defer p.connMutex.Unlock()

	p.logger.WithField("max_connections", p.config.MaxConnections).Info("Initializing AMQP connection pool")

	// Create connections to all hosts
	for i, host := range p.config.Hosts {
		for connNum := 0; connNum < p.config.MaxConnections/len(p.config.Hosts); connNum++ {
			conn, err := p.createConnection(host, i*100+connNum)
			if err != nil {
				p.logger.WithError(err).WithField("host", host).Error("Failed to create connection")
				continue
			}

			p.connections = append(p.connections, conn)
			atomic.AddInt64(&p.metrics.TotalConnections, 1)
			atomic.AddInt64(&p.metrics.ActiveConnections, 1)
		}
	}

	if len(p.connections) == 0 {
		return fmt.Errorf("failed to create any connections")
	}

	// Start health checker
	if p.healthChecker != nil {
		go p.healthChecker.start()
	}

	p.logger.WithField("connections", len(p.connections)).Info("AMQP connection pool initialized")
	return nil
}

// createConnection creates a new pooled connection
func (p *AMQPPool) createConnection(host string, index int) (*PooledConnection, error) {
	// Build connection URL
	var connURL string
	if p.config.TLS.Enabled {
		connURL = fmt.Sprintf("amqps://%s:%s@%s%s",
			p.config.Username, p.config.Password, host, p.config.VirtualHost)
	} else {
		connURL = fmt.Sprintf("amqp://%s:%s@%s%s",
			p.config.Username, p.config.Password, host, p.config.VirtualHost)
	}

	// Configure connection properties
	connConfig := amqp.Config{
		Heartbeat: p.config.Heartbeat,
		Locale:    "en_US",
	}

	// Configure TLS if enabled
	if p.config.TLS.Enabled {
		tlsConfig := &tls.Config{
			InsecureSkipVerify: p.config.TLS.SkipVerify,
		}

		if p.config.TLS.CertFile != "" && p.config.TLS.KeyFile != "" {
			cert, err := tls.LoadX509KeyPair(p.config.TLS.CertFile, p.config.TLS.KeyFile)
			if err != nil {
				return nil, fmt.Errorf("failed to load TLS certificate: %w", err)
			}
			tlsConfig.Certificates = []tls.Certificate{cert}
		}

		if p.config.TLS.CAFile != "" {
			roots, err := x509.SystemCertPool()
			if err != nil || roots == nil {
				roots = x509.NewCertPool()
			}

			caBytes, err := os.ReadFile(p.config.TLS.CAFile)
			if err != nil {
				return nil, fmt.Errorf("failed to read AMQP TLS CA file: %w", err)
			}
			if !roots.AppendCertsFromPEM(caBytes) {
				return nil, fmt.Errorf("failed to append AMQP TLS CA certificate")
			}
			tlsConfig.RootCAs = roots
		}

		connConfig.TLSClientConfig = tlsConfig
	}

	// Create connection with timeout
	dialer := &net.Dialer{Timeout: p.config.ConnectionTimeout}
	connConfig.Dial = dialer.Dial

	conn, err := amqp.DialConfig(connURL, connConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to %s: %w", host, err)
	}

	// Create channel pool for this connection
	channels := make(chan *PooledChannel, p.config.MaxChannelsPerConn)

	pooledConn := &PooledConnection{
		conn:      conn,
		channels:  channels,
		host:      host,
		healthy:   true,
		lastUsed:  time.Now(),
		created:   time.Now(),
		connIndex: index,
	}

	// Pre-create some channels
	for i := 0; i < 5; i++ {
		ch, err := p.createChannel(conn)
		if err != nil {
			p.logger.WithError(err).Warn("Failed to pre-create channel")
			continue
		}

		pooledCh := &PooledChannel{
			channel:     ch,
			inUse:       false,
			lastUsed:    time.Now(),
			confirmMode: p.config.PublishConfirm,
		}

		if p.config.PublishConfirm {
			if err := ch.Confirm(false); err != nil {
				p.logger.WithError(err).Warn("Failed to enable confirm mode")
			}
		}

		select {
		case channels <- pooledCh:
			atomic.AddInt64(&p.metrics.TotalChannels, 1)
		default:
			ch.Close()
		}
	}

	// Monitor connection for closure
	go p.monitorConnection(pooledConn)

	p.logger.WithFields(logrus.Fields{
		"host":     host,
		"index":    index,
		"channels": len(channels),
		"tls":      p.config.TLS.Enabled,
	}).Info("Created pooled AMQP connection")

	return pooledConn, nil
}

// createChannel creates a new AMQP channel with QoS settings
func (p *AMQPPool) createChannel(conn *amqp.Connection) (*amqp.Channel, error) {
	ch, err := conn.Channel()
	if err != nil {
		return nil, err
	}

	// Set QoS
	err = ch.Qos(
		p.config.PrefetchCount,
		p.config.PrefetchSize,
		p.config.GlobalQos,
	)
	if err != nil {
		ch.Close()
		return nil, fmt.Errorf("failed to set QoS: %w", err)
	}

	return ch, nil
}

// GetChannel gets a channel from the pool using load balancing
func (p *AMQPPool) GetChannel() (*PooledChannel, error) {
	p.connMutex.RLock()
	defer p.connMutex.RUnlock()

	if len(p.connections) == 0 {
		return nil, fmt.Errorf("no connections available")
	}

	// Select connection based on load balancing strategy
	var conn *PooledConnection
	switch p.config.LoadBalancing.Strategy {
	case "round_robin":
		conn = p.selectRoundRobin()
	case "least_used":
		conn = p.selectLeastUsed()
	case "random":
		conn = p.selectRandom()
	default:
		conn = p.selectRoundRobin()
	}

	if conn == nil {
		return nil, fmt.Errorf("no healthy connections available")
	}

	// Try to get a channel from the selected connection
	select {
	case ch := <-conn.channels:
		ch.mutex.Lock()
		ch.inUse = true
		ch.lastUsed = time.Now()
		ch.mutex.Unlock()

		conn.mutex.Lock()
		conn.lastUsed = time.Now()
		conn.mutex.Unlock()

		atomic.AddInt64(&p.metrics.ActiveChannels, 1)
		return ch, nil
	case <-time.After(time.Second):
		// Create a new channel if none available
		newCh, err := p.createChannel(conn.conn)
		if err != nil {
			return nil, fmt.Errorf("failed to create new channel: %w", err)
		}

		pooledCh := &PooledChannel{
			channel:     newCh,
			inUse:       true,
			lastUsed:    time.Now(),
			confirmMode: p.config.PublishConfirm,
		}

		if p.config.PublishConfirm {
			if err := newCh.Confirm(false); err != nil {
				newCh.Close()
				return nil, fmt.Errorf("failed to enable confirm mode: %w", err)
			}
		}

		atomic.AddInt64(&p.metrics.TotalChannels, 1)
		atomic.AddInt64(&p.metrics.ActiveChannels, 1)

		return pooledCh, nil
	}
}

// ReturnChannel returns a channel to the pool
func (p *AMQPPool) ReturnChannel(ch *PooledChannel) {
	if ch == nil {
		return
	}

	ch.mutex.Lock()
	ch.inUse = false
	ch.lastUsed = time.Now()
	ch.mutex.Unlock()

	atomic.AddInt64(&p.metrics.ActiveChannels, -1)

	// Find the connection this channel belongs to
	p.connMutex.RLock()
	defer p.connMutex.RUnlock()

	for _, conn := range p.connections {
		if conn.healthy {
			select {
			case conn.channels <- ch:
				return
			default:
				// Channel pool is full, close the channel
				ch.channel.Close()
				return
			}
		}
	}

	// No healthy connection found, close the channel
	ch.channel.Close()
}

// selectRoundRobin selects a connection using round-robin algorithm
func (p *AMQPPool) selectRoundRobin() *PooledConnection {
	if len(p.connections) == 0 {
		return nil
	}

	for i := 0; i < len(p.connections); i++ {
		index := atomic.AddUint64(&p.roundRobin, 1) % uint64(len(p.connections))
		conn := p.connections[index]

		conn.mutex.RLock()
		healthy := conn.healthy
		conn.mutex.RUnlock()

		if healthy {
			return conn
		}
	}

	return nil
}

// selectLeastUsed selects the connection used least recently
func (p *AMQPPool) selectLeastUsed() *PooledConnection {
	var selected *PooledConnection
	var oldestTime time.Time

	for _, conn := range p.connections {
		conn.mutex.RLock()
		if conn.healthy && (selected == nil || conn.lastUsed.Before(oldestTime)) {
			selected = conn
			oldestTime = conn.lastUsed
		}
		conn.mutex.RUnlock()
	}

	return selected
}

// selectRandom selects a random healthy connection
func (p *AMQPPool) selectRandom() *PooledConnection {
	healthyConns := make([]*PooledConnection, 0)

	for _, conn := range p.connections {
		conn.mutex.RLock()
		if conn.healthy {
			healthyConns = append(healthyConns, conn)
		}
		conn.mutex.RUnlock()
	}

	if len(healthyConns) == 0 {
		return nil
	}

	return healthyConns[rand.Intn(len(healthyConns))]
}

// PublishWithConfirm publishes a message with confirmation
func (p *AMQPPool) PublishWithConfirm(exchange, routingKey string, message []byte, headers amqp.Table) error {
	ch, err := p.GetChannel()
	if err != nil {
		atomic.AddInt64(&p.metrics.FailedPublishes, 1)
		return err
	}
	defer p.ReturnChannel(ch)

	// Publish message
	err = ch.channel.Publish(
		exchange,   // exchange
		routingKey, // routing key
		false,      // mandatory
		false,      // immediate
		amqp.Publishing{
			ContentType:  "application/json",
			Body:         message,
			DeliveryMode: amqp.Persistent,
			Timestamp:    time.Now(),
			Headers:      headers,
			Expiration:   fmt.Sprintf("%d", int64(p.config.MessageTTL.Milliseconds())),
		},
	)

	if err != nil {
		atomic.AddInt64(&p.metrics.FailedPublishes, 1)
		return fmt.Errorf("failed to publish message: %w", err)
	}

	// Wait for confirmation if enabled
	if ch.confirmMode {
		select {
		case confirm := <-ch.channel.NotifyPublish(make(chan amqp.Confirmation, 1)):
			if !confirm.Ack {
				atomic.AddInt64(&p.metrics.FailedPublishes, 1)
				return fmt.Errorf("message not acknowledged by broker")
			}
		case <-time.After(p.config.PublishTimeout):
			atomic.AddInt64(&p.metrics.FailedPublishes, 1)
			return fmt.Errorf("publish confirmation timeout")
		}
	}

	atomic.AddInt64(&p.metrics.PublishedMessages, 1)
	return nil
}

// monitorConnection monitors a connection for closure and health
func (p *AMQPPool) monitorConnection(conn *PooledConnection) {
	closeChan := make(chan *amqp.Error)
	conn.conn.NotifyClose(closeChan)

	for {
		select {
		case <-p.shutdownChan:
			return
		case err := <-closeChan:
			p.logger.WithError(err).WithField("host", conn.host).Warn("Connection closed, marking unhealthy")

			conn.mutex.Lock()
			conn.healthy = false
			conn.mutex.Unlock()

			atomic.AddInt64(&p.metrics.ActiveConnections, -1)

			// Attempt to reconnect
			go p.reconnectConnection(conn)
			return
		}
	}
}

// reconnectConnection attempts to reconnect a failed connection
func (p *AMQPPool) reconnectConnection(oldConn *PooledConnection) {
	backoff := p.config.ReconnectDelay
	attempts := 0

	for {
		if p.config.MaxReconnectAttempts > 0 && attempts >= p.config.MaxReconnectAttempts {
			p.logger.WithField("host", oldConn.host).Error("Max reconnection attempts reached")
			return
		}

		select {
		case <-p.shutdownChan:
			return
		case <-time.After(backoff):
			attempts++
			atomic.AddInt64(&p.metrics.ReconnectAttempts, 1)

			newConn, err := p.createConnection(oldConn.host, oldConn.connIndex)
			if err != nil {
				p.logger.WithError(err).WithField("attempt", attempts).Error("Reconnection failed")

				// Exponential backoff
				backoff = time.Duration(float64(backoff) * p.config.ReconnectMultiplier)
				if backoff > p.config.MaxReconnectDelay {
					backoff = p.config.MaxReconnectDelay
				}
				continue
			}

			// Replace the old connection
			p.connMutex.Lock()
			for i, conn := range p.connections {
				if conn == oldConn {
					p.connections[i] = newConn
					break
				}
			}
			p.connMutex.Unlock()

			atomic.AddInt64(&p.metrics.ActiveConnections, 1)

			p.logger.WithFields(logrus.Fields{
				"host":     oldConn.host,
				"attempts": attempts,
			}).Info("Connection reconnected successfully")

			return
		}
	}
}

// GetMetrics returns current pool metrics
func (p *AMQPPool) GetMetrics() AMQPMetrics {
	p.metrics.mutex.RLock()
	defer p.metrics.mutex.RUnlock()

	return AMQPMetrics{
		TotalConnections:  atomic.LoadInt64(&p.metrics.TotalConnections),
		ActiveConnections: atomic.LoadInt64(&p.metrics.ActiveConnections),
		TotalChannels:     atomic.LoadInt64(&p.metrics.TotalChannels),
		ActiveChannels:    atomic.LoadInt64(&p.metrics.ActiveChannels),
		PublishedMessages: atomic.LoadInt64(&p.metrics.PublishedMessages),
		FailedPublishes:   atomic.LoadInt64(&p.metrics.FailedPublishes),
		ReconnectAttempts: atomic.LoadInt64(&p.metrics.ReconnectAttempts),
	}
}

// Shutdown gracefully shuts down the connection pool
func (p *AMQPPool) Shutdown() error {
	p.shutdownOnce.Do(func() {
		close(p.shutdownChan)

		if p.healthChecker != nil {
			close(p.healthChecker.stopChan)
		}

		p.connMutex.Lock()
		defer p.connMutex.Unlock()

		for _, conn := range p.connections {
			// Close all channels
			close(conn.channels)
			for ch := range conn.channels {
				ch.channel.Close()
			}

			// Close connection
			conn.conn.Close()
		}

		p.logger.Info("AMQP connection pool shut down")
	})

	return nil
}

// start starts the health checker
func (hc *HealthChecker) start() {
	ticker := time.NewTicker(hc.interval)
	defer ticker.Stop()

	for {
		select {
		case <-hc.stopChan:
			return
		case <-ticker.C:
			hc.checkHealth()
		}
	}
}

// checkHealth performs health checks on all connections
func (hc *HealthChecker) checkHealth() {
	hc.pool.connMutex.RLock()
	connections := make([]*PooledConnection, len(hc.pool.connections))
	copy(connections, hc.pool.connections)
	hc.pool.connMutex.RUnlock()

	for _, conn := range connections {
		conn.mutex.RLock()
		healthy := conn.healthy
		conn.mutex.RUnlock()

		if !healthy {
			continue
		}

		// Try to create a temporary channel to test the connection
		ch, err := conn.conn.Channel()
		if err != nil {
			hc.pool.logger.WithError(err).WithField("host", conn.host).Warn("Connection health check failed")

			conn.mutex.Lock()
			conn.healthy = false
			conn.mutex.Unlock()

			continue
		}

		ch.Close()
	}
}
