module cymonkey-content-plugin-onnx

go 1.26.5

require (
	blockade v0.0.0
	github.com/webong/ext/pkg/plugin v0.5.1
	github.com/yalue/onnxruntime_go v1.35.0
)

require (
	github.com/kr/text v0.2.0 // indirect
	github.com/webong/ext/pkg/graph v0.1.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
	wirebridge v0.0.0
)

replace blockade => ../../lib/blockade

replace wirebridge => ../wirebridge
