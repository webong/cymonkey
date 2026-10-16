module console

go 1.26.5

require (
	blockade v0.0.0
	board v0.0.0
	jangolova v0.0.0
)

require github.com/webong/ext/pkg/plugin-go/contract v0.2.0

require (
	github.com/gorilla/websocket v1.5.3 // indirect
	github.com/kr/text v0.2.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)

replace blockade => ../blockade

replace board => ../board

replace jangolova => ../jangolova
