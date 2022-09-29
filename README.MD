# Gamma - a Minecraft bedrock Proxy

adaptation from [haveachin/bedprox](https://github.com/haveachin/bedprox)

## Added/changed Features

- TODO

## TODO
- Implement API
- Implement L7 protection

## Command-Line Flags

`-config-path` specifies the path to all your server configs [default: `"./configs/"`]

### Example Usage

`./gamma -config-path="."`

## Global config.yml
### Example/Default
```yaml
debug: false
genericJoinResponse: There is no proxy associated with this domain. Please check your configuration.
receiveProxyProtocol: false
prometheus:
  enabled: false
  bind: :9060
api:
  enabled: false
  bind: :5000
ping:
  edition: MCPE
  versionname: 1.19.30
  versionProtocol: 554
  description: Gamma proxy
  playerCount: 0
  maxPlayerCount: 10
  gamemode: SURVIVAL
  gamemodeNumeric: 1
```

Values can be left out if they don't deviate from the default, a config.json with just `{}` is still required for startup.
### Fields
- TODO

## Proxy Config

### Examples

#### Minimal Config

<details>
<summary>min.example.com</summary>

```json
{
  "domainNames": ["mc.example.com", "example.com"],
  "proxyTo": ":8080"
}
```

</details>

#### Full Config

<details>
<summary>full.example.com</summary>

```json
{
  "domains": ["mc.example.com", "example.com"],
  "listenTo": ":19132",
  "proxyTo": ":8080",
  "proxyProtocol": false,
  "dialTimeout": 1000,
  "dialTimeoutMessage": "Server is currently offline",
  "sendProxyProtocol": false
}
```

</details>

## Prometheus exporter
The built-in prometheus exporter can be used to view metrics about gamma' operation.
This can be used through `"prometheusEnabled": true` and `"prometheusBind": ":9070"` in `config.yml`
It is recommended to firewall the prometheus exporter with an application like *ufw* or *iptables* to make it only accessible by your own Prometheus instance.
### Prometheus configuration:
Example prometheus.yml configuration:
```yaml
scrape_configs:
  - job_name: gamma
    static_configs:
    - targets: ['gamma-exporter-hostname:port']
```

### Metrics:
* gamma_connected: show the amount of connected players per instance and proxy:
    * **Example response:** `gamma_connected{host="proxy.example.com",instance="vps1.example.com:9070",job="gamma"} 10`
    * **host:** listenTo domain as specified in the gamma configuration.
    * **instance:** what gamma instance the amount of players are connected to.
    * **job:** what job was specified in the prometheus configuration.
* gamma_handshakes: counter of the number of handshake packets received per instance, type and target:
    * **Example response:** `gamma_handshakes{instance="vps1.example.com:9070",type="status",host="proxy.example.com",country="DE"} 5`
    * **instance:** what gamma instance handshakes were received on.
    * **type:** the type of handshake received; "status" or "login".
    * **host:** the target host specified by the client (login only).

## API
### Route examples
GET `/proxies` will return
```json
[
"config",
"config2"
]
```

GET `/proxies/{name}` will return
```json
{
"domainNames": ["play.example.org"],
"proxyTo": "backend.example.org:25566"
}
```

POST `/proxies/{name}` with body
```json
{
"domainNames": ["play.example.org"],
"proxyTo": "backend.example.org:25566"
}
```
will return
```json
{"success": true, "message": "the proxy has been succesfully added"}
```

DELETE `/proxies/{name}` will return 200(OK)

GET `/` will return 200(OK)

## Used sources
- [haveachin/bedprox](https://github.com/haveachin/bedprox)
- [haveachin/infrared](https://github.com/haveachin/infrared)
- [lhridder/infrared](https://github.com/lhridder/infrared)
- [Sandertv/go-raknet](https://github.com/Sandertv/go-raknet)
- [Sandertv/gophertunnel](https://github.com/Sandertv/gophertunnel)