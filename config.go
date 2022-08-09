package gamma

import (
	"encoding/json"
	"github.com/fsnotify/fsnotify"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type GlobalConfig struct {
	ReceiveProxyProtocol bool   `json:"receiveProxyProtocol"`
	PrometheusEnabled    bool   `json:"prometheusEnabled"`
	PrometheusBind       string `json:"prometheusBind"`
	ApiEnabled           bool   `json:"apiEnabled"`
	ApiBind              string `json:"apiBind"`
	GenericJoinResponse  string `json:"genericJoinResponse"`
	PingEdition          string `json:"pingEdition"`
	PingVersionName      string `json:"pingVersionName"`
	PingDescription      string `json:"pingDescription"`
	PingVersionProtocol  int    `json:"pingVersionProtocol"`
	PingPlayerCount      int    `json:"pingPlayerCount"`
	PingMaxPlayerCount   int    `json:"pingMaxPlayerCount"`
	PingGamemode         string `json:"pingGamemode"`
	PingGamemodeNumeric  int    `json:"pingGamemodeNumeric"`
	Debug                bool   `json:"debug"`
}

type ProxyConfig struct {
	sync.RWMutex
	watcher *fsnotify.Watcher

	removeCallback func()
	changeCallback func()

	Domains            []string `json:"domains"`
	ListenTo           string   `json:"listenTo"`
	ProxyTo            string   `json:"proxyTo"`
	ProxyBind          string   `json:"proxyBind"`
	DialTimeout        int      `json:"dialTimeout"`
	DialTimeoutMessage string   `json:"dialTimeoutMessage"`
	SendProxyProtocol  bool     `json:"sendProxyProtocol"`
}

var GammaConfig GlobalConfig

var DefaultConfig = GlobalConfig{
	ReceiveProxyProtocol: false,
	PrometheusEnabled:    false,
	PrometheusBind:       ":9100",
	ApiEnabled:           false,
	ApiBind:              ":5000",
	GenericJoinResponse:  "There is no proxy associated with this domain. Please check your configuration.",
	PingEdition:          "MCPE",
	PingVersionName:      "Gamma",
	PingDescription:      "Gamma proxy",
	PingVersionProtocol:  527,
	PingPlayerCount:      0,
	PingMaxPlayerCount:   10,
	PingGamemode:         "SURVIVAL",
	PingGamemodeNumeric:  1,
	Debug:                false,
}

var DefaultProxyConfig = ProxyConfig{
	Domains:            []string{"localhost"},
	ListenTo:           ":19132",
	ProxyTo:            "localhost:19133",
	ProxyBind:          "",
	DialTimeout:        1000,
	DialTimeoutMessage: "Sorry but the server is offline.",
	SendProxyProtocol:  false,
}

func LoadGlobalConfig() error {
	jsonFile, err := os.Open("config.json")
	if err != nil {
		return err
	}
	var config = DefaultConfig
	jsonParser := json.NewDecoder(jsonFile)
	err = jsonParser.Decode(&config)
	if err != nil {
		return err
	}
	GammaConfig = config
	_ = jsonFile.Close()
	return nil
}

func readFilePaths(path string) ([]string, error) {
	var filePaths []string
	files, err := ioutil.ReadDir(path)
	if err != nil {
		return nil, err
	}

	for _, file := range files {
		if file.IsDir() {
			continue
		}

		filePaths = append(filePaths, filepath.Join(path, file.Name()))
	}

	return filePaths, err
}

func LoadProxyConfigsFromPath(path string) (map[string]ProxyConfig, error) {
	filePaths, err := readFilePaths(path)
	if err != nil {
		return nil, err
	}

	cfgs := make(map[string]ProxyConfig)

	for _, filePath := range filePaths {
		cfg, err := NewProxyConfigFromPath(filePath)
		if err != nil {
			return nil, err
		}
		cfgs[filePath] = cfg
	}

	return cfgs, nil
}

func NewProxyConfigFromPath(path string) (ProxyConfig, error) {
	log.Println("Loading", path)

	cfg, err := LoadFromPath(path)
	if err != nil {
		return ProxyConfig{}, err
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return ProxyConfig{}, err
	}
	cfg.watcher = watcher

	go func() {
		defer watcher.Close()
		log.Printf("Starting to watch %s", path)
		cfg.watch(path, time.Millisecond*50)
		log.Printf("Stopping to watch %s", path)
	}()

	if err := watcher.Add(path); err != nil {
		return ProxyConfig{}, err
	}

	return cfg, err
}

func LoadFromPath(path string) (ProxyConfig, error) {
	config := DefaultProxyConfig

	bb, err := ioutil.ReadFile(path)
	if err != nil {
		return ProxyConfig{}, err
	}

	if err := json.Unmarshal(bb, &config); err != nil {
		return ProxyConfig{}, err
	}

	return config, err
}

func WatchProxyConfigFolder(path string, out chan *ProxyConfig) error {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	defer watcher.Close()

	if err := watcher.Add(path); err != nil {
		return err
	}

	defer close(out)
	for {
		select {
		case event, ok := <-watcher.Events:
			if !ok {
				return nil
			}
			if event.Op&fsnotify.Create == fsnotify.Create && filepath.Ext(event.Name) == ".json" {
				proxyCfg, err := NewProxyConfigFromPath(event.Name)
				if err != nil {
					log.Printf("Failed loading %s; error %s", event.Name, err)
					continue
				}
				out <- &proxyCfg
			}
		case err, ok := <-watcher.Errors:
			if !ok {
				return nil
			}
			log.Printf("Failed watching %s; error %s", path, err)
		}
	}
}

func (cfg *ProxyConfig) watch(path string, interval time.Duration) {
	// The interval protects the watcher from write event spams
	// This is necessary due to how some text editors handle file safes
	tick := time.Tick(interval)
	var lastEvent *fsnotify.Event

	for {
		select {
		case <-tick:
			if lastEvent == nil {
				continue
			}
			cfg.onConfigWrite(*lastEvent)
			lastEvent = nil
		case event, ok := <-cfg.watcher.Events:
			if !ok {
				return
			}
			if event.Op&fsnotify.Remove == fsnotify.Remove {
				cfg.removeCallback()
				return
			}
			if event.Op&fsnotify.Write == fsnotify.Write {
				lastEvent = &event
			}
		case err, ok := <-cfg.watcher.Errors:
			if !ok {
				return
			}
			log.Printf("Failed watching %s; error %s", path, err)
		}
	}
}

func (cfg *ProxyConfig) onConfigWrite(event fsnotify.Event) {
	log.Println("Updating", event.Name)
	if err := cfg.LoadFromPath(event.Name); err != nil {
		log.Printf("Failed update on %s; error %s", event.Name, err)
		return
	}
	cfg.changeCallback()
}

// LoadFromPath loads the ProxyConfig from a file
func (cfg *ProxyConfig) LoadFromPath(path string) error {
	cfg.Lock()
	defer cfg.Unlock()

	var defaultCfg map[string]interface{}
	bb, err := json.Marshal(&DefaultProxyConfig)
	if err != nil {
		return err
	}

	if err := json.Unmarshal(bb, &defaultCfg); err != nil {
		return err
	}

	bb, err = ioutil.ReadFile(path)
	if err != nil {
		return err
	}

	var loadedCfg map[string]interface{}
	if err := json.Unmarshal(bb, &loadedCfg); err != nil {
		return err
	}

	for k, v := range loadedCfg {
		defaultCfg[k] = v
	}

	bb, err = json.Marshal(defaultCfg)
	if err != nil {
		return err
	}

	return json.Unmarshal(bb, cfg)
}
