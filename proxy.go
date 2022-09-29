package gamma

import (
	"fmt"
	"github.com/lhridder/gamma/protocol"
	"github.com/pires/go-proxyproto"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/sandertv/go-raknet"
	"log"
	"net"
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
	Config *ProxyConfig
	UID    string
	mu     sync.Mutex
	Dialer raknet.Dialer
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

func (proxy *Proxy) ProxyBind() string {
	proxy.Config.RLock()
	defer proxy.Config.RUnlock()
	return proxy.Config.ProxyBind
}

func (proxy *Proxy) UIDs() []string {
	var uids []string
	for _, domain := range proxy.DomainNames() {
		uid := proxyUID(domain, proxy.ListenTo())
		uids = append(uids, uid)
	}
	return uids
}

func proxyUID(domain, addr string) string {
	return fmt.Sprintf("%s@%s", strings.ToLower(domain), addr)
}

func (proxy *Proxy) Dial() (*raknet.Conn, error) {
	c, err := proxy.Dialer.Dial(proxy.Config.ProxyTo)
	if err != nil {
		return nil, err
	}
	return c, err
}

type proxyProtocolDialer struct {
	connAddr       net.Addr
	upstreamDialer raknet.UpstreamDialer
}

func (d proxyProtocolDialer) Dial(network, address string) (net.Conn, error) {
	rc, err := d.upstreamDialer.Dial(network, address)
	if err != nil {
		return nil, err
	}

	header := &proxyproto.Header{
		Version:           2,
		Command:           proxyproto.PROXY,
		TransportProtocol: proxyproto.UDPv4,
		SourceAddr:        d.connAddr.(*net.UDPAddr),
		DestinationAddr:   rc.RemoteAddr(),
	}

	if _, err = header.WriteTo(rc); err != nil {
		return rc, err
	}

	return rc, nil
}

func (proxy *Proxy) HandleLogin(conn protocol.ProcessedConn) error {
	if proxy.ProxyProtocol() {
		proxy.Dialer = raknet.Dialer{
			UpstreamDialer: &net.Dialer{
				Timeout: 5 * time.Second,
				LocalAddr: &net.UDPAddr{
					IP: net.ParseIP(proxy.ProxyBind()),
				},
			},
		}
		proxy.Dialer.UpstreamDialer = &proxyProtocolDialer{
			connAddr:       conn.RemoteAddr,
			upstreamDialer: proxy.Dialer.UpstreamDialer,
		}
	}

	rc, err := proxy.Dial()
	if err != nil {
		log.Printf("[i] %s did not respond to ping; is the target offline?", proxy.ProxyTo())
		err := conn.Disconnect(proxy.DisconnectMessage())
		if err != nil {
			return err
		}
		return nil
	}
	defer rc.Close()

	if _, err := rc.Write(conn.NetworkBytes); err != nil {
		rc.Close()
		return err
	}

	_, err = rc.ReadPacket()
	if err != nil {
		return err
	}

	if _, err := rc.Write(conn.ReadBytes); err != nil {
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
