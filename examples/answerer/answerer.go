package main

import (
	"context"
	"log"
	"time"

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
		},
		ConfigSignaling: datachannel.SignalingConfig{
			Active:  true,
			Address: ":8888",
		},
	})
	if err != nil {
		log.Fatalln(err)
	}
	ready := make(chan struct{})
	defer close(ready)
	err = dc.Start(context.Background(), &datachannel.Options{
		OnOpenDatachannel: func() error {
			ready <- struct{}{}
			return nil
		},
		OnMessageDatachannel: func(msg webrtc.DataChannelMessage, peerID string, datachannelID string) {
			if msg.IsString {
				log.Printf("[%s][%s]: %s\n", peerID, datachannelID, msg.Data)
			} else {
				log.Printf("[%s][%s]: %v\n", peerID, datachannelID, msg.Data)
			}
		},
	})
	if err != nil {
		log.Fatal(err)
	}

	log.Println("Waiting datachannel evevnt onOpen...")
	<-ready
	log.Println("Connected, now send data")

	dc.SendText("PC_TEST", "DC_1", "Hello world from answerer!")

	time.Sleep(5 * time.Second)
	dc.CreateDC("PC_TEST", "raw", datachannel.OnEvent{
		OnOpen: func() error {
			ready <- struct{}{}
			return nil
		},
		OnMessage: func(msg webrtc.DataChannelMessage, peerID string, datachannelID string) {
			if msg.IsString {
				log.Printf("[%s][%s]: %s\n", peerID, datachannelID, msg.Data)
			} else {
				log.Printf("[%s][%s]: %v\n", peerID, datachannelID, msg.Data)
			}
		},
	})
	<-ready
	dc.SendText("PC_TEST", "raw", "another channel")

	<-dc.Done()
}
