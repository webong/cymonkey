module cymonkey

go 1.26.5

require (
	blockade v0.0.0
	board v0.0.0
	console v0.0.0
	cymonkey-content-plugin-endpoints v0.0.0
	github.com/gorilla/websocket v1.5.3
	gopkg.in/yaml.v3 v3.0.1
	jangolova v0.0.0
)

require github.com/webong/ext/pkg/graph v0.1.0 // indirect

replace blockade => ./lib/blockade

replace console => ./lib/console

replace cymonkey-content-plugin-endpoints => ./pkg/content-endpoints

replace board => ./lib/board

replace jangolova => ./lib/jangolova

require (
	github.com/google/uuid v1.6.0 // indirect
	github.com/pion/datachannel v1.6.3 // indirect
	github.com/pion/dtls/v3 v3.1.9 // indirect
	github.com/pion/ice/v4 v4.4.4 // indirect
	github.com/pion/interceptor v0.1.49 // indirect
	github.com/pion/logging v0.2.4 // indirect
	github.com/pion/mdns/v2 v2.2.1 // indirect
	github.com/pion/randutil v0.1.0 // indirect
	github.com/pion/rtcp v1.2.18 // indirect
	github.com/pion/rtp v1.10.5 // indirect
	github.com/pion/sctp v1.11.3 // indirect
	github.com/pion/sdp/v3 v3.0.20 // indirect
	github.com/pion/srtp/v3 v3.1.0 // indirect
	github.com/pion/stun/v4 v4.0.1 // indirect
	github.com/pion/transport/v5 v5.1.1 // indirect
	github.com/pion/turn/v5 v5.1.2 // indirect
	github.com/pion/webrtc/v4 v4.2.22
	github.com/webong/ext/pkg/plugin v0.5.1
	github.com/wlynxg/anet v0.0.5 // indirect
	golang.org/x/crypto v0.48.0 // indirect
	golang.org/x/net v0.50.0 // indirect
	golang.org/x/sys v0.46.0 // indirect
	golang.org/x/time v0.14.0 // indirect
)
