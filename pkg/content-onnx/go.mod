module cymonkey-content-plugin-onnx

go 1.26.5

require (
	contentkit v0.0.0
	blockade v0.0.0
	github.com/yalue/onnxruntime_go v1.35.0
)

require (
	github.com/kr/text v0.2.0 // indirect
	github.com/webong/ext/pkg/plugin-go/contract v0.2.1
	gopkg.in/yaml.v3 v3.0.1 // indirect
)

replace blockade => ../blockade

replace contentkit => ../contentkit
