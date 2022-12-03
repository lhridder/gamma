package gamma

import (
	"encoding/json"
	"github.com/fsnotify/fsnotify"
	"gopkg.in/yaml.v2"
	"io/ioutil"
	"log"
	"path/filepath"
	"sync"
	"time"
)

type Service struct {
	Enabled bool   `yaml:"enabled"`
	Bind    string `yaml:"bind"`
}

type Ping struct {
	Edition         string `yaml:"edition"`
	VersionName     string `yaml:"versionName"`
	VersionProtocol int    `yaml:"versionProtocol"`
	Description     string `yaml:"description"`
	PlayerCount     int    `yaml:"playerCount"`
	MaxPlayerCount  int    `yaml:"maxPlayerCount"`
	Gamemode        string `yaml:"gamemode"`
	GamemodeNumeric int    `yaml:"gamemodeNumeric"`
}

type GlobalConfig struct {
	Prometheus           Service
	Api                  Service
	Ping                 Ping
	Debug                bool   `yaml:"debug"`
	ReceiveProxyProtocol bool   `yaml:"receiveProxyProtocol"`
	GenericJoinResponse  string `yaml:"genericJoinResponse"`
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
	Debug:                false,
	GenericJoinResponse:  "There is no proxy associated with this domain. Please check your configuration.",
	ReceiveProxyProtocol: false,
	Prometheus: Service{
		Enabled: false,
		Bind:    ":9060",
	},
	Api: Service{
		Enabled: false,
		Bind:    ":5000",
	},
	Ping: Ping{
		Edition:         "MCPE",
		VersionName:     "1.19.50",
		VersionProtocol: 560,
		Description:     "Gamma proxy",
		PlayerCount:     0,
		MaxPlayerCount:  10,
		Gamemode:        "SURVIVAL",
		GamemodeNumeric: 1,
	},
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
	log.Println("Loading config.yml")
	ymlFile, err := ioutil.ReadFile("config.yml")
	if err != nil {
		return err
	}
	var config = DefaultConfig
	err = yaml.Unmarshal(ymlFile, &config)
	if err != nil {
		return err
	}
	GammaConfig = config
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

func LoadProxyConfigsFromPath(path string) ([]*ProxyConfig, error) {
	filePaths, err := readFilePaths(path)
	if err != nil {
		return nil, err
	}

	var cfgs []*ProxyConfig

	for _, filePath := range filePaths {
		cfg, err := NewProxyConfigFromPath(filePath)
		if err != nil {
			return nil, err
		}
		cfgs = append(cfgs, cfg)
	}

	return cfgs, nil
}

func NewProxyConfigFromPath(path string) (*ProxyConfig, error) {
	log.Println("Loading", path)

	cfg, err := LoadFromPath(path)
	if err != nil {
		return nil, err
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	cfg.watcher = watcher

	go func() {
		defer watcher.Close()
		log.Printf("Starting to watch %s", path)
		cfg.watch(path, time.Millisecond*50)
		log.Printf("Stopping to watch %s", path)
	}()

	if err := watcher.Add(path); err != nil {
		return nil, err
	}

	return cfg, err
}

func LoadFromPath(path string) (*ProxyConfig, error) {
	var config *ProxyConfig

	defaultCfg, err := json.Marshal(&DefaultProxyConfig)
	if err != nil {
		return nil, err
	}

	err = json.Unmarshal(defaultCfg, &config)
	if err != nil {
		return nil, err
	}

	bb, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, err
	}

	if err := json.Unmarshal(bb, &config); err != nil {
		return nil, err
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
				out <- proxyCfg
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
