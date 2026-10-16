module cymonkey-content-plugin-endpoints

go 1.26.5

require (
	blockade v0.0.0
	github.com/gorilla/websocket v1.5.3
)

require (
	github.com/kr/text v0.2.0 // indirect
	github.com/webong/ext/pkg/plugin-go/contract v0.2.0
	gopkg.in/yaml.v3 v3.0.1 // indirect
	wirebridge v0.0.0
)

replace blockade => ../../lib/blockade

replace wirebridge => ../wirebridge
