module console

go 1.26.5

require github.com/webong/ext/pkg/plugin-go/contract v0.7.0

replace board => ../board

require (
	github.com/gorilla/websocket v1.5.3
	gopkg.in/yaml.v3 v3.0.1
	jangolova v0.0.0
)

replace jangolova => ../../lib/jangolova
