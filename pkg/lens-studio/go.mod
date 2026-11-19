module cymonkey-lens-studio

go 1.26.5

require (
	cymonkey v0.0.0
	jangolova v0.0.0
)

require github.com/gorilla/websocket v1.5.3 // indirect

replace cymonkey => ../..

replace jangolova => ../../res/jangolova
