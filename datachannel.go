package datachannel

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"sync"
	"time"

	webrtc "github.com/pion/webrtc/v3"
)

type sdpOffer struct {
	ID  string                     `json:"peer_id"`
	SDP *webrtc.SessionDescription `json:"sdp"`
}

type WebrtcDataChannel struct {
	sync.Mutex
	config       Config
	signaling    *Signaling
	apiWebrtc    *webrtc.API
	peers        map[string]*Peer
	optionsPeers *Options

	connected chan struct{}
	close     chan struct{}
	closeOnce sync.Once
}

var ErrConfigNull = errors.New("Config is null")

func New(config *Config) (*WebrtcDataChannel, error) {
	if config == nil {
		return nil, ErrConfigNull
	}
	v := WebrtcDataChannel{
		config:    *config,
		apiWebrtc: webrtc.NewAPI(),
		peers:     make(map[string]*Peer),
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

func (webrtcDC *WebrtcDataChannel) Close() <-chan struct{} {
	closed := make(chan struct{})

	go func(peers map[string]*Peer) {
		defer func() {
			fn := func() {
				close(webrtcDC.close)
				close(webrtcDC.connected)
				webrtcDC.signaling.Stop()
			}
			webrtcDC.closeOnce.Do(fn)
			close(closed)

		}()

		for _, peer := range peers {
			if err := peer.Close(); err != nil {
				log.Printf("Core Error close peer(id:%s):%v", peer.id, err)
			}
		}
	}(webrtcDC.peers)

	return closed
}

/* Implement Context interface */
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
func (webrtcDC *WebrtcDataChannel) Deadline() (deadline time.Time, ok bool) {
	return time.Time{}, false
}
func (webrtcDC *WebrtcDataChannel) Value(key interface{}) interface{} {
	return nil
}

/* Start functions */
type Options struct {
	OnOpenDatachannel    func() error
	OnMessageDatachannel func(msg webrtc.DataChannelMessage)
}

func (webrtcDC *WebrtcDataChannel) Start(ctx context.Context, options *Options) (err error) {
	err = webrtcDC.Err()
	if err != nil {
		return err
	}

	if webrtcDC.config.ConfigSignaling.Active {
		webrtcDC.signaling.Start()
		defer func() {
			if err != nil {
				webrtcDC.signaling.Stop()
			}
		}()
		// err = webrtcDC.startServer(ctx)
	}

	if options != nil {
		webrtcDC.optionsPeers = options
	}
	wg := sync.WaitGroup{}
	wg.Add(len(webrtcDC.config.ConfigWebRTC.PeerConfig))
	for _, peerConfig := range webrtcDC.config.ConfigWebRTC.PeerConfig {
		log.Printf("Attempting to connect to peer id:%s, sending the offer", peerConfig.ID)
		go func(config *PeerConfig, wg *sync.WaitGroup, options *Options, ctx context.Context) {
			defer func() {
				wg.Done()
			}()
			_, err = webrtcDC.sendOffer(config, options, ctx)
			if err != nil {
				log.Printf("Error when attempting to connect to peer %s:%v", config.ID, err)
				return
			}
		}(&peerConfig, &wg, options, ctx)

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
	wg.Wait()

	return err
}

func (webrtcDC *WebrtcDataChannel) addPeer(peer *Peer) error {
	webrtcDC.Lock()
	defer webrtcDC.Unlock()

	if err := webrtcDC.Err(); err != nil {
		return err
	}
	webrtcDC.peers[peer.id] = peer

	return nil
}

func (webrtcDC *WebrtcDataChannel) delPeer(peerId string) error {
	webrtcDC.Lock()
	defer webrtcDC.Unlock()

	if err := webrtcDC.Err(); err != nil {
		return err
	}
	delete(webrtcDC.peers, peerId)

	return nil
}

func (webrtcDC *WebrtcDataChannel) CreateDC(peerID string, channelID string, onevent OnEvent) (*DC, error) {
	peer, found := webrtcDC.peers[peerID]
	if !found {
		log.Printf("not found peer for id='%s'\n", peerID)
		return nil, ErrNotFoundPC
	}

	var dc *DC
	_, found = peer.dataChannels[channelID]
	if found {
		log.Printf("channel id='%s' already create to peer %s\n", channelID, peerID)
		return nil, ErrNotFoundDC
	}
	datachannel, err := peer.peerConnection.CreateDataChannel(channelID, nil)
	if err != nil {
		return nil, err
	}
	dc = NewDC(peerID, datachannel, onevent)
	peer.addDC(channelID, dc)
	return dc, nil
}

func (webrtcDC *WebrtcDataChannel) GetDC(peerID string, channelID string) (*DC, error) {
	peer, found := webrtcDC.peers[peerID]
	if !found {
		log.Printf("not found peer for id='%s'\n", peerID)
		return nil, ErrNotFoundPC
	}

	var dc *DC
	dc, found = peer.dataChannels[channelID]
	if !found {
		log.Printf("not found channel id='%s' in this peer\n", channelID)
		return nil, ErrNotFoundDC
	}
	return dc, nil
}

/*
* routeOffer is an HTTP handler for the "/offer" route.
* It listens for incoming SDP offer requests from clients, processes them, and responds with an SDP answer.
*
* When a client sends an SDP offer, this handler performs the following steps:
*
* 1. Receives the SDP offer from the client.
* 2. Processes the SDP offer to negotiate session parameters.
* 3. Generates an SDP answer to the client's offer.
* 4. Sends the SDP answer as a response to the client.
*
* This function plays a crucial role in establishing WebRTC communication by handling SDP offer/answer exchanges.
 */
func (webrtcDC *WebrtcDataChannel) routeOffer(w http.ResponseWriter, req *http.Request) {

	method := req.Method
	if method == http.MethodOptions {
		addCors(w, webrtcDC.config.ConfigSignaling.AllowOrigin)
		log.Println("Processed HTTP OPTIONS request")
		return
	}

	if method != http.MethodOptions &&
		method != http.MethodGet &&
		method != http.MethodPost {
		replyStatus(w, http.StatusMethodNotAllowed)
		log.Printf("Error: Invalid HTTP method received: %s\n", method)
		return
	}
	buf, err := io.ReadAll(req.Body)
	if err != nil {
		replyStatus(w, http.StatusBadRequest)
		log.Printf("Error reading request body: %v\n", err)
		return
	}

	var jsep sdpOffer

	if err := json.Unmarshal(buf, &jsep); err != nil {
		replyStatus(w, http.StatusBadRequest)
		log.Printf("Error parsing request JSEP format: %v\n", err)
		return
	}

	log.Printf("Received a POST method on /offer to create a new peer connection with id:%s\n", jsep.ID)

	if _, found := webrtcDC.peers[jsep.ID]; found {
		log.Printf("Peer with ID %s already exists in peers\n", jsep.ID)
		replyStatus(w, http.StatusFound)
		return
	}

	var peerConnection *webrtc.PeerConnection
	peerConnection, err = webrtc.NewPeerConnection(webrtcDC.config.ConfigWebRTC.Config)
	if err != nil {
		log.Println("New peer connection error:", err)
		replyStatus(w, http.StatusBadRequest)
		return
	}

	peer := NewPeer(jsep.ID, peerConnection, webrtcDC.delPeer)
	webrtcDC.addPeer(peer)

	peerConnection.OnDataChannel(func(dc *webrtc.DataChannel) {
		webrtcDC.handleOnDataChannel(dc, peer)
	})

	err = peerConnection.SetRemoteDescription(*jsep.SDP)
	if err != nil {
		replyStatus(w, http.StatusBadRequest)
		log.Println("set remote description error:", err)
		return
	}

	peerConnection.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		log.Println("peer connection state:", state)
		if state == webrtc.PeerConnectionStateConnected {
			webrtcDC.connected <- struct{}{}
		}

	})

	iceCandidateCompleteChan := webrtc.GatheringCompletePromise(peerConnection)
	answer, err := peerConnection.CreateAnswer(nil)
	if err != nil {
		log.Fatal(err)
	}

	err = peerConnection.SetLocalDescription(answer)
	if err != nil {
		replyStatus(w, http.StatusBadRequest)
		log.Printf("Error setting local description: %v\n", err)
		return
	}
	<-iceCandidateCompleteChan
	var answerRaw []byte
	answerRaw, err = json.Marshal(peerConnection.LocalDescription())
	if err != nil {
		replyStatus(w, http.StatusBadRequest)
		log.Printf("Error marshaling answer: %v\n", err)
		return
	}

	allowOrigin := webrtcDC.config.ConfigSignaling.AllowOrigin
	if allowOrigin != "" {
		addCors(w, allowOrigin)
	}

	w.Header().Set("Content-Type", "application/json")

	if n, err := w.Write(answerRaw); err != nil {
		log.Println("Error writing response: ", err)
	} else if n != len(answerRaw) {
		log.Printf("Error: Written length (%d) is different from buffer length (%d)\n", n, len(answerRaw))
	}
}

/*
* sendOffer initiates the process of creating an SDP offer and sending it to the signaling server.
* It performs the following steps:
*
* 1. Generate an SDP offer to negotiate session parameters with the remote peer.
* 2. Send the SDP offer to the signaling server via an HTTP POST request.
* 3. Wait for the SDP response from the server, which will contain session details from the remote peer.
* 4. Initialize the WebRTC peer connection with the received SDP response.
*
* This function plays a crucial role in establishing WebRTC communication with the remote peer.
 */
func (webrtcDC *WebrtcDataChannel) sendOffer(config *PeerConfig, options *Options, ctx context.Context) (*webrtc.SessionDescription, error) {
	err := webrtcDC.Err()
	if err != nil {
		return nil, err
	}
	var peerConnection *webrtc.PeerConnection
	peerConnection, err = webrtc.NewPeerConnection(webrtcDC.config.ConfigWebRTC.Config)
	if err != nil {
		log.Printf("[%s]  Error creating a new PeerConnection: %v\n", config.ID, err)
		return nil, err
	}

	peer := NewPeer(config.ID, peerConnection, webrtcDC.delPeer)
	webrtcDC.addPeer(peer)

	peerConnection.OnDataChannel(func(dc *webrtc.DataChannel) {
		webrtcDC.handleOnDataChannel(dc, peer)
	})

	for _, label := range config.DataChannelLabels {
		log.Printf("[%s]    Creation of data channel with label: %s", config.ID, label)
		var dataChannel *webrtc.DataChannel
		dataChannel, err = peerConnection.CreateDataChannel(label, nil)
		if err != nil {
			log.Fatal(err)
		}

		var onopen func() error
		var onmessage func(msg webrtc.DataChannelMessage)
		if options != nil {
			onopen = options.OnOpenDatachannel
			onmessage = options.OnMessageDatachannel
		}
		peer.addDC(dataChannel.Label(), NewDC(config.ID, dataChannel, OnEvent{
			OnOpen:    onopen,
			OnClose:   peer.delDC,
			OnMessage: onmessage,
		}))

		// dataChannel.OnMessage(func(msg webrtc.DataChannelMessage) {
		// 	log.Printf("[%s][%s] Message received on datachannel : %s\n", config.ID, dataChannel.Label(), string(msg.Data))
		// })
	}

	peerConnection.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		log.Println("Peer connection state changed to: ", state)
		if state == webrtc.PeerConnectionStateConnected {
			webrtcDC.connected <- struct{}{}
		}
	})

	iceCandidateCompleteChan := webrtc.GatheringCompletePromise(peerConnection)
	offer, err := peerConnection.CreateOffer(nil)
	if err != nil {
		log.Fatal(err)
	}
	err = peerConnection.SetLocalDescription(offer)
	if err != nil {
		log.Fatal(err)
	}

	<-iceCandidateCompleteChan

	var offerRaw []byte
	offerRaw, err = json.Marshal(sdpOffer{
		ID:  config.ID,
		SDP: peerConnection.LocalDescription(),
	})
	if err != nil {
		return nil, err
	}

	buff := bytes.NewBuffer(offerRaw)
	client := http.Client{Timeout: 30 * time.Second}
	var resp *http.Response
	resp, err = client.Post(config.SignalingServerTarget+"/offer", "application/json", buff)
	if err != nil {
		return nil, err
	}
	var answer webrtc.SessionDescription

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Printf("Error reading request body: %v\n", err)
		return nil, err
	}
	if err := json.Unmarshal(data, &answer); err != nil {
		log.Printf("Error parsing request answer: %v\n", err)
		return nil, err
	}

	peerConnection.SetRemoteDescription(answer)
	return &answer, nil
}

func (webrtcDC *WebrtcDataChannel) routeCandidate(w http.ResponseWriter, req *http.Request) {
	log.Println("ROUTE candidate TODO")
}
func (webrtcDC *WebrtcDataChannel) routeCandidates(w http.ResponseWriter, req *http.Request) {
	log.Println("ROUTE candidates TODO")
}

var (
	ErrNotFoundPC = errors.New("peerconnection not found")
	ErrNotFoundDC = errors.New("datachannel not found")
)

func (webrtcDC *WebrtcDataChannel) SendText(peerID string, channelID string, msg string) error {
	peer, found := webrtcDC.peers[peerID]
	if !found {
		log.Printf("not found peer for id='%s'\n", peerID)
		return ErrNotFoundPC
	}

	var dc *DC
	dc, found = peer.dataChannels[channelID]
	if !found {
		log.Printf("not found channel id='%s' in this peer\n", channelID)
		return ErrNotFoundDC
	}
	err := dc.SendText(msg)
	if err != nil {
		log.Printf("datachannel send error:%v\n", err)
		return err

	}

	return nil
}

func (webrtcDC *WebrtcDataChannel) handleOnDataChannel(dc *webrtc.DataChannel, peer *Peer) {
	log.Printf("[%s] Received onDataChannel(label:%s) event", peer.id, dc.Label())
	var onopen func() error
	var onmessage func(msg webrtc.DataChannelMessage)
	if webrtcDC.optionsPeers != nil {
		onopen = webrtcDC.optionsPeers.OnOpenDatachannel
		onmessage = webrtcDC.optionsPeers.OnMessageDatachannel
	}
	peer.addDC(dc.Label(), NewDC(peer.id, dc, OnEvent{
		OnOpen:    onopen,
		OnClose:   peer.delDC,
		OnMessage: onmessage,
	}))
}
