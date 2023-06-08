package datachannel

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"time"

	webrtc "github.com/pion/webrtc/v3"
)

type WebrtcDataChannel struct {
	config         Config
	signaling      *Signaling
	apiWebrtc      *webrtc.API
	peerConnection *webrtc.PeerConnection
	dataChannel    *webrtc.DataChannel

	connected chan struct{}
	close     chan struct{}
}

func New(config *Config) (*WebrtcDataChannel, error) {
	if config.Type != TypeOfferer && config.Type != TypeAnswerer {
		return nil, fmt.Errorf("invalid type:%d", config.Type)
	}
	v := WebrtcDataChannel{
		config:    *config,
		apiWebrtc: webrtc.NewAPI(),
		connected: make(chan struct{}),
		close:     make(chan struct{}),
	}
	signaling, err := NewSignaling(&v, &SignalingConfig{
		Provider: &v,
		Address:  config.ConfigSignaling.Address,
		Routes: map[string]func(w http.ResponseWriter, _ *http.Request){
			"/offer":      v.routeOffer,
			"/candidate":  v.routeCandidate,
			"/candidates": v.routeCandidates,
		},
	})
	if err != nil {
		return nil, err
	}
	v.signaling = signaling

	return &v, nil
}

func (webrtcDC *WebrtcDataChannel) Done() <-chan struct{} {
	return webrtcDC.close
}
func (webrtcDC *WebrtcDataChannel) Err() error {
	select {
	case <-webrtcDC.close:
		return fmt.Errorf("run canceled")
	default:
		return nil
	}
}

// Deadline implements context.Context
func (webrtcDC *WebrtcDataChannel) Deadline() (deadline time.Time, ok bool) {
	return time.Time{}, false
}
func (webrtcDC *WebrtcDataChannel) Value(key interface{}) interface{} {
	return nil
}

func (webrtcDC *WebrtcDataChannel) Start(ctx context.Context) (err error) {
	err = webrtcDC.Err()
	if err != nil {
		return err
	}

	switch webrtcDC.config.Type {
	case TypeOfferer:
		_, err = webrtcDC.sendOffer(ctx)
	case TypeAnswerer:
		webrtcDC.signaling.Start()
		defer func() {
			if err != nil {
				webrtcDC.signaling.Stop()
			}
		}()
		err = webrtcDC.startServer(ctx)
	default:
		err = fmt.Errorf("invalid type:%d", webrtcDC.config.Type)
	}

	if err != nil {
		return err
	}

	select {
	case <-ctx.Done():
		err = ctx.Err()
	case <-webrtcDC.Done():
		err = webrtcDC.Err()
	case <-webrtcDC.connected:
		err = nil
	}

	if err != nil {
		return err
	}
	return err
}

func (webrtcDC *WebrtcDataChannel) startClient(ctx context.Context) error {
	webrtcDC.sendOffer(ctx)
	return nil
}

func (webrtcDC *WebrtcDataChannel) startServer(ctx context.Context) error {
	return nil
}

func (webrtcDC *WebrtcDataChannel) Connected() <-chan struct{} {
	return webrtcDC.connected
}

func (webrtcDC *WebrtcDataChannel) SendText(msg string) {
	if webrtcDC.dataChannel == nil {
		log.Println("datachannel is null ?!?")
		return
	}
	err := webrtcDC.dataChannel.SendText(msg)
	if err != nil {
		log.Printf("datachannel send error:%v\n", err)
		return

	}
}

/*
* Signaling routes handlers
 */
func (webrtcDC *WebrtcDataChannel) routeOffer(w http.ResponseWriter, req *http.Request) {
	log.Println("ROUTE OFFER")
	buf, err := ioutil.ReadAll(req.Body)
	if err != nil {
		replyStatus(w, http.StatusBadRequest)
		log.Printf("Reading body request error: %v", err)
		return
	}
	var jsep webrtc.SessionDescription

	if err := json.Unmarshal(buf, &jsep); err != nil {
		replyStatus(w, http.StatusBadRequest)
		log.Printf("request jsep format error %s", err.Error())
		return
	}

	if webrtcDC.peerConnection != nil {
		replyStatus(w, http.StatusBadRequest)
		log.Printf("peer connection already create")
		return
	}

	webrtcDC.peerConnection, err = webrtc.NewPeerConnection(webrtcDC.config.ConfigWebrtc.Config)
	if err != nil {
		replyStatus(w, http.StatusBadRequest)
		log.Println("new peer connection error:", err)
		return
	}

	webrtcDC.peerConnection.OnDataChannel(func(dc *webrtc.DataChannel) {
		log.Println("on datachannel:", dc)
		webrtcDC.dataChannel = dc
		webrtcDC.connected <- struct{}{}
	})

	// Créer un DataChannel
	var dataChannel *webrtc.DataChannel
	dataChannel, err = webrtcDC.peerConnection.CreateDataChannel(webrtcDC.config.ConfigWebrtc.DataChannelID, nil)
	if err != nil {
		log.Fatal(err)
	}

	dataChannel.OnMessage(func(msg webrtc.DataChannelMessage) {
		log.Printf("Message received on datachannel : %s\n", string(msg.Data))
	})

	err = webrtcDC.peerConnection.SetRemoteDescription(jsep)
	if err != nil {
		replyStatus(w, http.StatusBadRequest)
		log.Println("set remote description error:", err)
		return
	}

	webrtcDC.peerConnection.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		log.Println("peer connection state:", state)

	})
	iceCandidateCompleteChan := webrtc.GatheringCompletePromise(webrtcDC.peerConnection)

	answer, err := webrtcDC.peerConnection.CreateAnswer(nil)
	if err != nil {
		log.Fatal(err)
	}

	err = webrtcDC.peerConnection.SetLocalDescription(answer)
	if err != nil {
		replyStatus(w, http.StatusBadRequest)
		log.Println("set local description error:", err)
		return
	}
	<-iceCandidateCompleteChan
	log.Println("localdesc:", webrtcDC.peerConnection.LocalDescription())
	var answerRaw []byte
	answerRaw, err = json.Marshal(webrtcDC.peerConnection.LocalDescription())
	if err != nil {
		replyStatus(w, http.StatusBadRequest)
		log.Println("marshal answer error:", err)
		return
	}

	if n, err := w.Write(answerRaw); err != nil {
		log.Println("write response error:", err)
	} else if n != len(answerRaw) {
		log.Printf("len writen (%d) is different than buffer (%d)\n", n, len(answerRaw))
	}

}
func (webrtcDC *WebrtcDataChannel) sendOffer(ctx context.Context) (*webrtc.SessionDescription, error) {
	err := webrtcDC.Err()
	if err != nil {
		return nil, err
	}
	webrtcDC.peerConnection, err = webrtc.NewPeerConnection(webrtcDC.config.ConfigWebrtc.Config)
	if err != nil {
		log.Println("new peer connection error:", err)
		return nil, err
	}

	webrtcDC.peerConnection.OnDataChannel(func(dc *webrtc.DataChannel) {
		log.Println("on datachannel:", dc)
		webrtcDC.dataChannel = dc
		webrtcDC.connected <- struct{}{}
	})

	// Créer un DataChannel
	var dataChannel *webrtc.DataChannel
	dataChannel, err = webrtcDC.peerConnection.CreateDataChannel(webrtcDC.config.ConfigWebrtc.DataChannelID, nil)
	if err != nil {
		log.Fatal(err)
	}
	// dataChannel.OnDial(func() {
	// 	log.Println("test write datachannel")
	// 	err := dataChannel.SendText("test datachannel ondial !!!!!!!!!!!!!")
	// 	if err != nil {
	// 		log.Println("send data err:", err)
	// 	}
	// })
	dataChannel.OnMessage(func(msg webrtc.DataChannelMessage) {
		log.Printf("Message received on datachannel : %s\n", string(msg.Data))
	})

	webrtcDC.peerConnection.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		log.Println("peer connection state:", state)

	})
	iceCandidateCompleteChan := webrtc.GatheringCompletePromise(webrtcDC.peerConnection)
	// Générer l'offre SDP
	offer, err := webrtcDC.peerConnection.CreateOffer(nil)
	if err != nil {
		log.Fatal(err)
	}

	// Définir la description de l'offre SDP en tant que description locale de PeerConnection
	err = webrtcDC.peerConnection.SetLocalDescription(offer)
	if err != nil {
		log.Fatal(err)
	}

	// jsonData := map[string]interface{}{
	// 	"jsep": peerConnection.LocalDescription(),
	// }
	<-iceCandidateCompleteChan

	var offerRaw []byte
	offerRaw, err = json.Marshal(webrtcDC.peerConnection.LocalDescription())
	if err != nil {
		return nil, err
	}

	log.Println(webrtcDC.peerConnection.LocalDescription())
	buff := bytes.NewBuffer(offerRaw)
	client := http.Client{Timeout: 30 * time.Second}
	var resp *http.Response
	resp, err = client.Post("http://localhost:8888/offer", "application/json", buff)
	if err != nil {
		return nil, err
	}
	var answer webrtc.SessionDescription

	data, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		log.Printf("Reading body request error: %v", err)
		return nil, err
	}
	log.Println(string(data))
	if err := json.Unmarshal(data, &answer); err != nil {
		log.Printf("request answer format error %s", err.Error())
		return nil, err
	}

	webrtcDC.peerConnection.SetRemoteDescription(answer)
	return &answer, nil
}

func (webrtcDC *WebrtcDataChannel) routeCandidate(w http.ResponseWriter, req *http.Request) {
	log.Println("ROUTE candidate TODO")
}
func (webrtcDC *WebrtcDataChannel) routeCandidates(w http.ResponseWriter, req *http.Request) {
	log.Println("ROUTE candidates TODO")
}
