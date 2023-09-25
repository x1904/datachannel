package datachannel

import (
	"net/http"

	webrtc "github.com/pion/webrtc/v3"
)

type Config struct {
	ConfigWebRTC    WebRTCConfig
	ConfigSignaling SignalingConfig
}

type PeerConfig struct {
	ID                    string
	DataChannelLabels     []string
	SignalingServerTarget string //http://localhost:8888
}

type WebRTCConfig struct {
	Config     webrtc.Configuration
	PeerConfig []PeerConfig
}

type SignalingConfig struct {
	Active      bool
	Provider    *WebrtcDataChannel
	Address     string
	Routes      map[string]func(w http.ResponseWriter, _ *http.Request)
	AllowOrigin string
}
