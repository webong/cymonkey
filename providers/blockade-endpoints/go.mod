module blockade-provider-endpoints

go 1.26.5

require (
	blockade v0.0.0
	providerplugin v0.0.0
)

require (
	github.com/kr/text v0.2.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)

replace blockade => ../../lib/blockade

replace providerplugin => ../../lib/plugin
