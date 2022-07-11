package gamma

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"github.com/lhridder/gamma/protocol"
	"github.com/lhridder/gamma/protocol/login"
	"github.com/pires/go-proxyproto"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/sandertv/go-raknet"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

var (
	handshakeCount = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "gamma_handshakes",
		Help: "The total number of handshakes made to each proxy by type",
	}, []string{"type", "host"})
)

type Gateway struct {
	listeners            sync.Map
	Proxies              sync.Map
	closed               chan bool
	wg                   sync.WaitGroup
	ReceiveProxyProtocol bool
	underAttack          bool
	connections          int
}

func (gateway *Gateway) KeepProcessActive() {
	gateway.wg.Wait()
}

func (gateway *Gateway) EnablePrometheus(bind string) error {
	gateway.wg.Add(1)

	go func() {
		defer gateway.wg.Done()

		http.Handle("/metrics", promhttp.Handler())
		err := http.ListenAndServe(bind, nil)
		if err != nil {
			panic(err)
		}
	}()

	log.Println("Enabling Prometheus metrics endpoint on", bind)
	return nil
}

func (gateway *Gateway) Close() {
	gateway.listeners.Range(func(k, v interface{}) bool {
		gateway.closed <- true
		_ = v.(*raknet.Listener).Close()
		return false
	})
}

func (gateway *Gateway) CloseProxy(proxyUID string) {
	log.Println("Closing config with UID", proxyUID)
	v, ok := gateway.Proxies.Load(proxyUID)
	if !ok {
		return
	}
	proxy := v.(*Proxy)

	uids := proxy.DomainNames()
	for _, uid := range uids {
		log.Println("Closing proxy with UID", uid)
		gateway.Proxies.Delete(uid)
	}

	playersConnected.DeleteLabelValues(proxy.DomainName())

	closeListener := true
	gateway.Proxies.Range(func(k, v interface{}) bool {
		otherProxy := v.(*Proxy)
		if proxy.ListenTo() == otherProxy.ListenTo() {
			closeListener = false
			return false
		}
		return true
	})

	if !closeListener {
		return
	}

	v, ok = gateway.listeners.Load(proxy.ListenTo())
	if !ok {
		return
	}
	_ = v.(*raknet.Listener).Close()
}

func (gateway *Gateway) RegisterProxy(proxy *Proxy) error {
	// Register new Proxy
	uids := proxy.DomainNames()
	for _, uid := range uids {
		log.Println("Registering proxy with UID", uid)
		gateway.Proxies.Store(uid, proxy)
	}
	proxyUID := proxy.UID

	proxy.Config.removeCallback = func() {
		gateway.CloseProxy(proxyUID)
	}

	proxy.Config.changeCallback = func() {
		gateway.CloseProxy(proxyUID)
		if err := gateway.RegisterProxy(proxy); err != nil {
			log.Println(err)
		}
	}

	playersConnected.WithLabelValues(proxy.DomainName())

	// Check if a gate is already listening to the Proxy address
	addr := proxy.ListenTo()
	if _, ok := gateway.listeners.Load(addr); ok {
		return nil
	}

	log.Println("Creating listener on", addr)
	listener, err := raknet.Listen(addr)
	if err != nil {
		return err
	}
	gateway.listeners.Store(addr, listener)

	listener.PongData(marshalPong(listener))

	gateway.wg.Add(1)
	go func() {
		if err := gateway.listenAndServe(listener, addr); err != nil {
			log.Printf("Failed to listen on %s; error: %s", proxy.ListenTo(), err)
		}
	}()
	return nil
}

func marshalPong(l *raknet.Listener) []byte {
	motd := strings.Split(GammaConfig.PingDescription, "\n")
	motd1 := motd[0]
	motd2 := ""
	if len(motd) > 1 {
		motd2 = motd[1]
	}

	port := l.Addr().(*net.UDPAddr).Port
	return []byte(fmt.Sprintf("%v;%v;%v;%v;%v;%v;%v;%v;%v;%v;%v;%v;",
		GammaConfig.PingEdition, motd1, GammaConfig.PingVersionProtocol, GammaConfig.PingVersionName, GammaConfig.PingPlayerCount, GammaConfig.PingMaxPlayerCount,
		l.ID(), motd2, GammaConfig.PingGamemode, GammaConfig.PingGamemodeNumeric, port, port))
}

func (gateway *Gateway) ListenAndServe(proxies []*Proxy) error {
	if len(proxies) <= 0 {
		return errors.New("no proxies in gateway")
	}

	/*
		if GammaConfig.UnderAttack {
			log.Println("Enabled permanent underAttack mode")
			gateway.underAttack = true
			underAttackStatus.Set(1)
		}

	*/

	gateway.closed = make(chan bool, len(proxies))

	for _, proxy := range proxies {
		if err := gateway.RegisterProxy(proxy); err != nil {
			gateway.Close()
			return err
		}
	}

	log.Println("All proxies are online")
	return nil
}

func (gateway *Gateway) listenAndServe(listener *raknet.Listener, addr string) error {
	defer gateway.wg.Done()

	for {
		conn, err := listener.Accept()
		if err != nil {
			log.Println(err)
		}

		go func() {
			if GammaConfig.Debug {
				log.Printf("[>] Incoming %s on listener %s", conn.RemoteAddr(), addr)
			}
			defer conn.Close()
			_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
			if err := gateway.serve(conn, addr); err != nil {

				if GammaConfig.Debug {
					log.Printf("[x] %s closed connection with %s; error: %s", conn.RemoteAddr(), addr, err)
				}
				return
			}
			_ = conn.SetDeadline(time.Time{})
			if GammaConfig.Debug {
				log.Printf("[x] %s closed connection with %s", conn.RemoteAddr(), addr)
			}
		}()
	}
}

func (gateway *Gateway) serve(conn net.Conn, addr string) (rerr error) {
	defer func() {
		if r := recover(); r != nil {
			switch x := r.(type) {
			case string:
				rerr = errors.New(x)
			case error:
				rerr = x
			default:
				rerr = errors.New("unknown panic in client handler")
			}
		}
	}()

	pc := protocol.ProcessedConn{
		Conn:       conn.(*raknet.Conn),
		RemoteAddr: conn.RemoteAddr(),
	}

	if gateway.ReceiveProxyProtocol {
		header, err := proxyproto.Read(bufio.NewReader(conn))
		if err != nil {
			return err
		}
		pc.RemoteAddr = header.SourceAddr
	}

	b, err := pc.ReadPacket()
	if err != nil {
		return err
	}
	pc.ReadBytes = b

	decoder := protocol.NewDecoder(bytes.NewReader(b))
	pks, err := decoder.Decode()
	if err != nil {
		return err
	}

	if len(pks) < 1 {
		return errors.New("no valid packets received")
	}

	var loginPk protocol.Login
	if err := protocol.UnmarshalPacket(pks[0], &loginPk); err != nil {
		return err
	}

	iData, cData, err := login.Parse(loginPk.ConnectionRequest)
	if err != nil {
		return err
	}
	pc.Username = iData.DisplayName
	pc.ServerAddr = cData.ServerAddress

	if strings.Contains(pc.ServerAddr, ":") {
		pc.ServerAddr, _, err = net.SplitHostPort(pc.ServerAddr)
		if err != nil {
			return err
		}
	}

	handshakeCount.With(prometheus.Labels{"type": "login", "host": pc.ServerAddr}).Inc()

	proxyUID := proxyUID(pc.ServerAddr, addr)
	if GammaConfig.Debug {
		log.Printf("[i] %s requests proxy with UID %s", pc.RemoteAddr, proxyUID)
	}

	v, ok := gateway.Proxies.Load(pc.ServerAddr)
	if !ok {
		err = pc.Disconnect(GammaConfig.GenericJoinResponse)
		if err != nil {
			return err
		}
		return nil
	}

	proxy := v.(*Proxy)

	_ = conn.SetDeadline(time.Time{})

	err = proxy.HandleLogin(pc)
	if err != nil {
		return err
	}

	return nil
}
