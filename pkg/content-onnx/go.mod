module cymonkey-content-plugin-onnx

go 1.26.5

require github.com/yalue/onnxruntime_go v1.35.0

require (
	github.com/kr/text v0.2.0 // indirect
	github.com/webong/ext/pkg/plugin-go/contract v0.4.0
	gopkg.in/yaml.v3 v3.0.1 // indirect
)

require jangolova v0.0.0

replace jangolova => ../../lib/jangolova
