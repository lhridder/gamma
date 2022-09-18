package main

import (
	"flag"
	"github.com/lhridder/gamma"
	"github.com/sandertv/go-raknet"
	"log"
	"net"
	"os"
	"time"
)

const (
	envPrefix     = "INFRARED_"
	envConfigPath = envPrefix + "CONFIG_PATH"
	clfConfigPath = "config-path"
)

var configPath = "./configs"

func envString(name string, value string) string {
	envString := os.Getenv(name)
	if envString == "" {
		return value
	}

	return envString
}

func initEnv() {
	configPath = envString(envConfigPath, configPath)
}

func initFlags() {
	flag.StringVar(&configPath, clfConfigPath, configPath, "path of all proxy configs")
	flag.Parse()
}

func init() {
	initEnv()
	initFlags()
}

func main() {
	log.Println("Starting gamma")

	err := gamma.LoadGlobalConfig()
	if err != nil {
		log.Println(err)
		return
	}

	log.Println("Loading configs folder")
	cfgs, err := gamma.LoadProxyConfigsFromPath("./configs")
	if err != nil {
		log.Printf("Failed loading proxy configs, error: %s", err)
		return
	}

	var proxies []*gamma.Proxy
	for _, cfg := range cfgs {
		proxies = append(proxies, &gamma.Proxy{
			Config: cfg,
			Dialer: raknet.Dialer{
				UpstreamDialer: &net.Dialer{
					Timeout: 5 * time.Second,
					LocalAddr: &net.TCPAddr{
						IP: net.ParseIP(cfg.ProxyBind),
					},
				},
			},
		})
	}

	outCfgs := make(chan *gamma.ProxyConfig)
	go func() {
		if err := gamma.WatchProxyConfigFolder("./configs", outCfgs); err != nil {
			log.Println("Failed watching config folder; error:", err)
			log.Println("SYSTEM FAILURE: CONFIG WATCHER FAILED")
		}
	}()

	log.Println("Starting gateway")
	gateway := gamma.Gateway{ReceiveProxyProtocol: gamma.GammaConfig.ReceiveProxyProtocol}

	go func() {
		for {
			cfg, ok := <-outCfgs
			if !ok {
				return
			}

			proxy := &gamma.Proxy{
				Config: cfg,
				UID:    cfg.Domains[0],
				Dialer: raknet.Dialer{
					UpstreamDialer: &net.Dialer{
						Timeout: 5 * time.Second,
					},
				},
			}
			if err := gateway.RegisterProxy(proxy); err != nil {
				log.Println("Failed registering proxy; error:", err)
			}
		}
	}()

	if gamma.GammaConfig.Prometheus.Enabled {
		err := gateway.EnablePrometheus(gamma.GammaConfig.Prometheus.Bind)
		if err != nil {
			log.Println(err)
			return
		}
	}

	log.Println("Starting Gamma")
	if err := gateway.ListenAndServe(proxies); err != nil {
		log.Fatal("Gateway exited; error: ", err)
	}

	gateway.KeepProcessActive()

}
