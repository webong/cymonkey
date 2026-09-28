module cymonkey

go 1.26.5

require (
	board v0.0.0
	blockade v0.0.0
	providerplugin v0.0.0
	github.com/google/go-cmp v0.7.0
	github.com/gorilla/websocket v1.5.3
	gopkg.in/yaml.v3 v3.0.1
	jangolova v0.0.0
)

replace blockade => ./lib/blockade

replace board => ./lib/board

replace providerplugin => ./lib/plugin

replace jangolova => ./lib/jangolova

require (
	github.com/kr/pretty v0.3.1 // indirect
	github.com/rogpeppe/go-internal v1.14.1 // indirect
	github.com/yalue/onnxruntime_go v1.35.0 // indirect
	gopkg.in/check.v1 v1.0.0-20201130134442-10cb98267c6c // indirect
)
