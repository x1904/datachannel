package main

import (
	"context"
	"log"
	"malwarux/datachannel"

	"github.com/pion/webrtc/v3"
)

func main() {

	dc, err := datachannel.New(&datachannel.Config{
		Type: datachannel.TypeAnswerer,
		ConfigWebrtc: datachannel.WebrtcConfig{
			Config: webrtc.Configuration{
				ICEServers: []webrtc.ICEServer{
					{
						URLs: []string{"stun:stun.l.google.com:19302"},
					},
				},
			},
			DataChannelID: "test",
		},
		ConfigSignaling: datachannel.SignalingConfig{
			Address: ":8888",
		},
	})
	if err != nil {
		log.Fatalln(err)
	}
	err = dc.Start(context.Background())
	if err != nil {
		log.Fatal(err)
	}

	log.Println("Connected, now send data")
	dc.SendText("Hello world from answerer!")

	<-dc.Done()
}
