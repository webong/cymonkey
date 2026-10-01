module cymonkey-content-plugin-onnx

go 1.26.5

require (
	blockade v0.0.0
	providerplugin v0.0.0
	github.com/yalue/onnxruntime_go v1.35.0
)

replace blockade => ../../lib/blockade

replace providerplugin => ../../lib/plugin
