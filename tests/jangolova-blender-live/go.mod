module cymonkey-blender-live

go 1.26.5

require (
	board v0.0.0
	github.com/gorilla/websocket v1.5.3
	jangolova v0.0.0
)

replace board => ../../pkg/board

replace jangolova => ../../lib/jangolova
