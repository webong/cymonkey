module cymonkey-effect-house

go 1.26.5

require (
	board v0.0.0
	jangolova v0.0.0
)

require github.com/gorilla/websocket v1.5.3 // indirect

replace board => ../board

replace jangolova => ../../lib/jangolova
