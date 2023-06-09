package datachannel

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"sync"
	"time"

	webrtc "github.com/pion/webrtc/v3"
)

type ID string
type NAME string

type WebrtcDataChannel struct {
	sync.Mutex
	config        Config
	signaling     *Signaling
	apiWebrtc     *webrtc.API
	pcControlling map[ID]*webrtc.PeerConnection
	pcControlled  map[ID]*webrtc.PeerConnection
	dataChannel   *webrtc.DataChannel

	connected chan struct{}
	close     chan struct{}
}

func New(config *Config) (*WebrtcDataChannel, error) {
	if config.Type != TypeOfferer && config.Type != TypeAnswerer && config.Type != TypeGateway {
		return nil, fmt.Errorf("invalid type:%d", config.Type)
	}
	v := WebrtcDataChannel{
		config:        *config,
		apiWebrtc:     webrtc.NewAPI(),
		pcControlling: make(map[ID]*webrtc.PeerConnection),
		pcControlled:  make(map[ID]*webrtc.PeerConnection),
		connected:     make(chan struct{}),
		close:         make(chan struct{}),
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
		for _, addr := range webrtcDC.config.ConfigSignaling.Addresses {
			_, err = webrtcDC.sendOffer(ctx, addr)
		}
	case TypeAnswerer:
		webrtcDC.signaling.Start()
		defer func() {
			if err != nil {
				webrtcDC.signaling.Stop()
			}
		}()
	case TypeGateway:
		webrtcDC.signaling.Start()
		defer func() {
			if err != nil {
				webrtcDC.signaling.Stop()
			}
		}()
		for _, addr := range webrtcDC.config.ConfigSignaling.Addresses {
			_, err = webrtcDC.sendOffer(ctx, addr)
		}
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

func (webrtcDC *WebrtcDataChannel) Connected() <-chan struct{} {
	return webrtcDC.connected
}

func (webrtcDC *WebrtcDataChannel) SendText(msg string) error {
	// webrtcDC.Lock()
	// channel, found := webrtcDC.dataChannel[NAME(channelName)]
	// webrtcDC.Unlock()

	// if !found {
	// 	return errors.New("channel not found")
	// }
	// channel.SendText(msg)
	if webrtcDC.dataChannel == nil {
		return errors.New("no datachannel")
	}
	webrtcDC.dataChannel.SendText(msg)
	return nil
}

/*
* Signaling routes handlers
 */
func (webrtcDC *WebrtcDataChannel) routeOffer(w http.ResponseWriter, req *http.Request) {
	log.Println("ROUTE OFFER")
	method := req.Method
	if method == http.MethodOptions {
		addCors(w, webrtcDC.config.ConfigSignaling.AllowOrigin)
		return
	}

	if method != http.MethodOptions &&
		method != http.MethodGet &&
		method != http.MethodPost {
		replyStatus(w, http.StatusMethodNotAllowed)
		return
	}
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

	var pc *webrtc.PeerConnection
	pc, err = webrtc.NewPeerConnection(webrtcDC.config.ConfigWebrtc.Config)
	if err != nil {
		replyStatus(w, http.StatusBadRequest)
		log.Println("new peer connection error:", err)
		return
	}

	id := ID(req.Proto + req.RemoteAddr)
	log.Println("proto:", req.Proto, "remote addr:", req.RemoteAddr)
	webrtcDC.Lock()
	if _, found := webrtcDC.pcControlled[id]; !found {
		log.Println("Adding")
		webrtcDC.pcControlled[id] = pc
	}
	webrtcDC.Unlock()

	pc.OnDataChannel(func(dc *webrtc.DataChannel) {
		log.Println("on datachannel:", dc)
		webrtcDC.dataChannel = dc
		webrtcDC.connected <- struct{}{}
	})

	// Créer un DataChannel
	var dataChannel *webrtc.DataChannel
	dataChannel, err = pc.CreateDataChannel(webrtcDC.config.ConfigWebrtc.DataChannelID, nil)
	if err != nil {
		log.Fatal(err)
	}

	dataChannel.OnMessage(func(msg webrtc.DataChannelMessage) {
		log.Printf("Message received on datachannel : %s\n", string(msg.Data))
	})

	err = pc.SetRemoteDescription(jsep)
	if err != nil {
		replyStatus(w, http.StatusBadRequest)
		log.Println("set remote description error:", err)
		return
	}

	pc.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		log.Println("peer connection state:", state)
		switch state {
		case webrtc.PeerConnectionStateFailed:
			fallthrough
		case webrtc.PeerConnectionStateDisconnected:
			pc.Close()
			webrtcDC.Lock()
			for id, peerconnection := range webrtcDC.pcControlled {
				if peerconnection == pc {
					delete(webrtcDC.pcControlled, id)
					break
				}
			}
			webrtcDC.Unlock()
		}

	})
	iceCandidateCompleteChan := webrtc.GatheringCompletePromise(pc)

	answer, err := pc.CreateAnswer(nil)
	if err != nil {
		log.Fatal(err)
	}

	err = pc.SetLocalDescription(answer)
	if err != nil {
		replyStatus(w, http.StatusBadRequest)
		log.Println("set local description error:", err)
		return
	}
	<-iceCandidateCompleteChan
	log.Println("localdesc:", pc.LocalDescription())
	var answerRaw []byte
	answerRaw, err = json.Marshal(pc.LocalDescription())
	if err != nil {
		replyStatus(w, http.StatusBadRequest)
		log.Println("marshal answer error:", err)
		return
	}

	allowOrigin := webrtcDC.config.ConfigSignaling.AllowOrigin
	if allowOrigin != "" {
		addCors(w, allowOrigin)
	}

	w.Header().Set("Content-Type", "application/json")

	if n, err := w.Write(answerRaw); err != nil {
		log.Println("write response error:", err)
	} else if n != len(answerRaw) {
		log.Printf("len writen (%d) is different than buffer (%d)\n", n, len(answerRaw))
	}

}
func (webrtcDC *WebrtcDataChannel) sendOffer(ctx context.Context, addr string) (*webrtc.SessionDescription, error) {
	err := webrtcDC.Err()
	if err != nil {
		return nil, err
	}

	var pc *webrtc.PeerConnection
	pc, err = webrtc.NewPeerConnection(webrtcDC.config.ConfigWebrtc.Config)
	if err != nil {
		log.Println("new peer connection error:", err)
		return nil, err
	}

	id := ID(addr)
	log.Println(addr)
	webrtcDC.Lock()
	if _, found := webrtcDC.pcControlling[id]; !found {
		log.Println("Adding new peerconnection (controlling)")
		webrtcDC.pcControlling[id] = pc
	}
	webrtcDC.Unlock()

	pc.OnDataChannel(func(dc *webrtc.DataChannel) {
		log.Println("on datachannel:", dc)
		webrtcDC.dataChannel = dc
		webrtcDC.connected <- struct{}{}
	})

	// Créer un DataChannel
	var dataChannel *webrtc.DataChannel
	dataChannel, err = pc.CreateDataChannel(webrtcDC.config.ConfigWebrtc.DataChannelID, nil)
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

	pc.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		switch state {
		case webrtc.PeerConnectionStateFailed:
			fallthrough
		case webrtc.PeerConnectionStateDisconnected:
			pc.Close()
			webrtcDC.Lock()
			for id, peerconnection := range webrtcDC.pcControlling {
				if peerconnection == pc {
					delete(webrtcDC.pcControlling, id)
					break
				}
			}
			webrtcDC.Unlock()
		}
	})
	iceCandidateCompleteChan := webrtc.GatheringCompletePromise(pc)
	// Générer l'offre SDP
	offer, err := pc.CreateOffer(nil)
	if err != nil {
		log.Fatal(err)
	}

	// Définir la description de l'offre SDP en tant que description locale de PeerConnection
	err = pc.SetLocalDescription(offer)
	if err != nil {
		log.Fatal(err)
	}

	// jsonData := map[string]interface{}{
	// 	"jsep": peerConnection.LocalDescription(),
	// }
	<-iceCandidateCompleteChan

	var offerRaw []byte
	offerRaw, err = json.Marshal(pc.LocalDescription())
	if err != nil {
		return nil, err
	}

	log.Println(pc.LocalDescription())
	buff := bytes.NewBuffer(offerRaw)
	client := http.Client{Timeout: 30 * time.Second}
	var resp *http.Response
	resp, err = client.Post("http://"+addr+"/offer", "application/json", buff)
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

	pc.SetRemoteDescription(answer)
	return &answer, nil
}

func (webrtcDC *WebrtcDataChannel) routeCandidate(w http.ResponseWriter, req *http.Request) {
	log.Println("ROUTE candidate TODO")
}
func (webrtcDC *WebrtcDataChannel) routeCandidates(w http.ResponseWriter, req *http.Request) {
	log.Println("ROUTE candidates TODO")
}
