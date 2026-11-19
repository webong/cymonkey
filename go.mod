module cymonkey

go 1.26.5

require (
	github.com/gorilla/websocket v1.5.3
	gopkg.in/yaml.v3 v3.0.1
)

require (
	github.com/webong/ext/pkg/ext-go/engine v0.4.0
	github.com/webong/ext/res/web/host v0.2.1
)

require jangolova v0.0.0

replace jangolova => ./res/jangolova
