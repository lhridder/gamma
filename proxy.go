package gamma

import (
	"fmt"
	"github.com/lhridder/gamma/protocol"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"log"
	"strings"
	"sync"
	"time"
)

var (
	playersConnected = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "gamma_connected",
		Help: "The total number of connected players",
	}, []string{"host"})
)

type Proxy struct {
	Config            *ProxyConfig
	UID               string
	cancelTimeoutFunc func()
	mu                sync.Mutex

	/*
		cacheOnlineTime   time.Time
		cacheStatusTime   time.Time
		cacheResponse     status.ClientBoundResponse
		cacheOnlineStatus bool
	*/
}

func (proxy *Proxy) DomainNames() []string {
	proxy.Config.RLock()
	defer proxy.Config.RUnlock()
	return proxy.Config.Domains
}

func (proxy *Proxy) DomainName() string {
	proxy.Config.RLock()
	defer proxy.Config.RUnlock()
	return proxy.Config.Domains[0]
}

func (proxy *Proxy) ListenTo() string {
	proxy.Config.RLock()
	defer proxy.Config.RUnlock()
	return proxy.Config.ListenTo
}

func (proxy *Proxy) ProxyTo() string {
	proxy.Config.RLock()
	defer proxy.Config.RUnlock()
	return proxy.Config.ProxyTo
}

func (proxy *Proxy) DisconnectMessage() string {
	proxy.Config.RLock()
	defer proxy.Config.RUnlock()
	return proxy.Config.DialTimeoutMessage
}

func (proxy *Proxy) Timeout() time.Duration {
	proxy.Config.RLock()
	defer proxy.Config.RUnlock()
	return time.Duration(proxy.Config.DialTimeout) * time.Millisecond
}

func (proxy *Proxy) ProxyProtocol() bool {
	proxy.Config.RLock()
	defer proxy.Config.RUnlock()
	return proxy.Config.SendProxyProtocol
}

func proxyUID(domain, addr string) string {
	return fmt.Sprintf("%s@%s", strings.ToLower(domain), addr)
}

func (proxy *Proxy) HandleLogin(conn protocol.ProcessedConn) error {
	rc, err := proxy.Config.dialer.DialTimeout(proxy.ProxyTo(), proxy.Timeout())
	defer rc.Close()
	if err != nil {
		err = conn.Disconnect(proxy.DisconnectMessage())
		if err != nil {
			return err
		}
		log.Printf("[i] %s did not respond to ping; is the target offline?", proxy.ProxyTo())
		return err
	}

	if _, err := rc.Write(conn.ReadBytes); err != nil {
		log.Println(err)
		rc.Close()
		return err
	}
	playersConnected.With(prometheus.Labels{"host": proxy.DomainName()}).Inc()
	defer playersConnected.With(prometheus.Labels{"host": proxy.DomainName()}).Dec()

	go func() {
		for {
			pk, err := rc.ReadPacket()
			if err != nil {
				return
			}
			_, err = conn.Write(pk)
			if err != nil {
				return
			}
		}
	}()
	for {
		pk, err := conn.ReadPacket()
		if err != nil {
			return err
		}
		_, err = rc.Write(pk)
		if err != nil {
			return err
		}
	}
}
