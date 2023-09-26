package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/pion/webrtc/v3"
	"github.com/x1904/datachannel"
)

func main() {

	dc, err := datachannel.New(&datachannel.Config{
		ConfigWebRTC: datachannel.WebRTCConfig{
			Config: webrtc.Configuration{
				ICEServers: []webrtc.ICEServer{
					{
						URLs: []string{"stun:stun.l.google.com:19302"},
					},
				},
			},
			PeerConfig: []datachannel.PeerConfig{
				{
					ID:                    "PC_TEST",
					DataChannelLabels:     []string{"DC_1"},
					SignalingServerTarget: "http://localhost:8888",
				},
			},
		},
	})
	if err != nil {
		log.Fatalln(err)
	}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-stop
		<-dc.Close()
	}()

	ready := make(chan struct{})
	defer close(ready)

	if err = dc.Start(context.Background(), &datachannel.Options{
		OnOpenDatachannel: func() error {
			ready <- struct{}{}
			return nil
		},
		OnMessageDatachannel: func(msg webrtc.DataChannelMessage, peerID string, datachannelID string) {
			if msg.IsString {
				log.Printf("[%s][%s]: %s\n", peerID, datachannelID, msg.Data)
			} else {
				log.Printf("[%s][%s]: %v\n", peerID, datachannelID, msg.Data[16:])
			}
		},
	}); err != nil {
		log.Fatal(err)
	}

	channel, errDC := dc.GetDC("PC_TEST", "DC_1")
	if errDC != nil {
		log.Fatalln(errDC)
	}

	<-ready
	log.Printf("DataChannel[label:%s] Connected, now send data\n", channel.Label())

	err = channel.SendText("Hello world!")
	if err == datachannel.ErrDcNotConnected {
		log.Fatalln(err)

	}

	<-dc.Done()
}
