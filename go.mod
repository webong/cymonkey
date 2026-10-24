module cymonkey

go 1.26.5

require (
	github.com/gorilla/websocket v1.5.3
	gopkg.in/yaml.v3 v3.0.1
)

require (
	github.com/webong/ext/pkg/plugin-go/contract v0.7.0
	github.com/webong/ext/res/web v0.5.0
)

require jangolova v0.0.0

replace jangolova => ./lib/jangolova
