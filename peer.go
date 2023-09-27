package datachannel

import (
	"errors"
	"fmt"
	"log"
	"sync"
	"sync/atomic"
	"time"

	webrtc "github.com/pion/webrtc/v3"
)

var ErrDcNotConnected = errors.New("datachannel not connected")

type OnEvent struct {
	OnOpen    func() error
	OnClose   func(label string) error
	OnMessage func(msg webrtc.DataChannelMessage, peerID string, datachannelID string)
}

type DC struct {
	provider    string
	ready       atomic.Bool
	datachannel *webrtc.DataChannel
	connected   chan struct{}
	onopen      func() error
	onclose     func(label string) error
	onmessage   func(msg webrtc.DataChannelMessage, peerID string, datachannelID string)
	close       chan struct{}
}

func NewDC(peerId string, datachannel *webrtc.DataChannel, onevent OnEvent) *DC {
	dc := &DC{
		provider:    peerId,
		datachannel: datachannel,
		connected:   make(chan struct{}),
		onopen:      onevent.OnOpen,
		onclose:     onevent.OnClose,
		onmessage:   onevent.OnMessage,
		close:       make(chan struct{}),
	}
	datachannel.OnOpen(func() {
		log.Printf("[%s][%s] OnOpen event detected\n", peerId, datachannel.Label())
		dc.ready.Store(true)
		// close(dc.connected)
		if dc.onopen != nil {
			dc.onopen()
		}
	})
	datachannel.OnMessage(func(msg webrtc.DataChannelMessage) {
		log.Printf("[%s][%s] Message received, call event onmessage(%p)\n", peerId, dc.Label(), dc.onmessage)
		if dc.onmessage != nil {
			dc.onmessage(msg, dc.provider, dc.Label())
		}
	})
	// if dc.onmessage != nil {
	// datachannel.OnMessage(dc.onmessage)
	// }
	return dc
}

func (datachannel *DC) Close() error {
	if err := datachannel.Err(); err != nil {
		log.Printf("[%s][%s] Context Error:%v", datachannel.provider, datachannel.Label(), err)
		return err
	}
	defer func() {
		close(datachannel.connected)
		close(datachannel.close)
	}()

	label := datachannel.Label()

	datachannel.datachannel.OnClose(func() {
		log.Printf("[%s][%s] closed\n", datachannel.provider, label)
	})
	if err := datachannel.datachannel.Close(); err != nil {
		log.Printf("[%s][%s] Error datachannel Close():%v", datachannel.provider, label, err)
		return err
	}

	if datachannel.onclose != nil {
		if err := datachannel.onclose(label); err != nil {
			log.Printf("[%s][%s] Error datachannel onclose():%v", datachannel.provider, label, err)
			return err
		}
	}
	return nil
}

func (datachannel *DC) Done() <-chan struct{} {
	return datachannel.close
}
func (datachannel *DC) Err() error {
	select {
	case <-datachannel.close:
		return fmt.Errorf("run canceled")
	default:
		return nil
	}
}

// Deadline implements context.Context
func (datachannel *DC) Deadline() (deadline time.Time, ok bool) {
	return time.Time{}, false
}
func (datachannel *DC) Value(key interface{}) interface{} {
	return nil
}
func (datachannel *DC) Connected() bool {
	if datachannel.ready.Load() {
		return true
	}
	select {
	case <-datachannel.connected:
		datachannel.ready.Store(true)
		return true
	case <-datachannel.Done():
		return false
	case <-time.After(3 * time.Second):
		return false
	}
}
func (datachannel *DC) Ready() <-chan struct{} {
	ch := make(chan struct{})
	go func(chan<- struct{}) {
		for i := 0; i < 3; i++ {
			if datachannel.Connected() {
				close(ch)
				return
			}
			log.Printf("Ready() run go func() loop_%d connected, sleep 1 sec\n", i)
			time.Sleep(1 * time.Second)
		}
	}(ch)
	return ch
}
func (datachannel *DC) Label() string {
	return datachannel.datachannel.Label()
}

func (datachannel *DC) SendText(msg string) error {
	if !datachannel.Connected() {
		return ErrDcNotConnected
	}
	return datachannel.datachannel.SendText(msg)
}

func (datachannel *DC) Send(data []byte) error {
	if !datachannel.Connected() {
		return ErrDcNotConnected
	}
	return datachannel.datachannel.Send(data)
}

type Peer struct {
	sync.Mutex
	id             string
	peerConnection *webrtc.PeerConnection
	dataChannels   map[string]*DC
	onclose        func(id string) error
}

func NewPeer(id string, pc *webrtc.PeerConnection, onclose func(id string) error) *Peer {
	return &Peer{
		id:             id,
		peerConnection: pc,
		dataChannels:   make(map[string]*DC),
		onclose:        onclose,
	}
}

func (p *Peer) Close() error {
	var err error

	for _, dc := range p.dataChannels {
		label := dc.Label()
		log.Printf("[%s] Closing datachannel label:'%s'...\n", p.id, label)
		err = dc.Close()
		if err != nil {
			log.Printf("[%s] Error close datachannel label:'%s':%v\n", p.id, label, err)
		}
	}

	log.Printf("[%s] Closing peerconnection:...\n", p.id)
	err = p.peerConnection.Close()
	if err != nil {
		log.Printf("[%s] Error close peerconnection:%v\n", p.id, err)
		return err
	}

	if p.onclose != nil {
		err = p.onclose(p.id)
		if err != nil {
			log.Printf("[%s] Error onclose peer:%v\n", p.id, err)

			return err
		}
	}
	return nil
}

var ErrDataChannelAlreadyExists = errors.New("satachannel already exists")

func (p *Peer) addDC(id string, dc *DC) error {
	p.Lock()
	defer p.Unlock()
	if p.dataChannels == nil {
		p.dataChannels = make(map[string]*DC)
	}
	if _, found := p.dataChannels[id]; found {
		return ErrDataChannelAlreadyExists
	}
	p.dataChannels[id] = dc
	return nil
}

func (p *Peer) delDC(id string) error {
	p.Lock()
	defer p.Unlock()
	if p.dataChannels == nil {
		return nil
	}
	if _, found := p.dataChannels[id]; !found {
		return ErrNotFoundDC
	}
	delete(p.dataChannels, id)
	return nil
}
