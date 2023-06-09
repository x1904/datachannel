package datachannel

import (
	"net/http"

	webrtc "github.com/pion/webrtc/v3"
)

type Config struct {
	Type            Type
	ConfigWebrtc    WebrtcConfig
	ConfigSignaling SignalingConfig
}

type WebrtcConfig struct {
	Config        webrtc.Configuration
	DataChannelID string
}

type SignalingConfig struct {
	Provider    *WebrtcDataChannel
	Address     string /* HTTP server to receive offer*/
	Routes      map[string]func(w http.ResponseWriter, _ *http.Request)
	Addresses   []string /* addresses to send offer*/
	AllowOrigin string
}
