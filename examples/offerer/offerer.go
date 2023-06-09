package main

import (
	"context"
	"log"

	"github.com/pion/webrtc/v3"
	"github.com/x1904/datachannel"
)

func main() {

	dc, err := datachannel.New(&datachannel.Config{
		Type: datachannel.TypeOfferer,
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
	})
	if err != nil {
		log.Fatalln(err)
	}
	err = dc.Start(context.Background())
	if err != nil {
		log.Fatal(err)
	}

	log.Println("Connected, now send data")
	dc.SendText("Hello world!")

	<-dc.Done()
}
