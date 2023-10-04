package main

import (
	"context"
	"log"
	"os"

	"github.com/pion/webrtc/v3"
	"github.com/x1904/datachannel"
	"github.com/x1904/datachannel/protocol"
)

func main() {
	if len(os.Args) < 2 {
		log.Fatalln("Usage: ./send_file [FILEPATH]")
	}

	filepath := os.Args[1]
	file, err := os.OpenFile(filepath, os.O_RDWR|os.O_CREATE, 0644)
	if err != nil {
		log.Fatalln(err)
	}

	manager, err := datachannel.New(&datachannel.Config{
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
					DataChannelLabels:     []string{"GLOBAL"},
					SignalingServerTarget: "http://localhost:8888",
				},
			},
		},
	})

	ready := make(chan struct{})
	defer close(ready)

	if err = manager.Start(context.Background(), &datachannel.Options{
		OnOpenDatachannel: func() error {
			ready <- struct{}{}
			return nil
		},
	}); err != nil {
		log.Fatal(err)
	}

	<-ready

	manager.CreateDC("PC_TEST", "DC_FILE_TRANSFERT", datachannel.OnEvent{
		OnOpen: func() error {
			ready <- struct{}{}
			return nil
		},
	})
	<-ready

	dc, errDC := manager.GetDC("PC_TEST", "DC_FILE_TRANSFERT")
	if errDC != nil {
		log.Fatalln(errDC)
	}

	log.Printf("DataChannel[label:%s] Connected, now send data\n", dc.Label())

	header := protocol.Header{
		Type: protocol.TypeData,
		Data: protocol.MetaData{
			Type: protocol.DataTypeBIN32,
			Name: generateNameFile(filepath),
		},
	}
	buffer := [8192]byte{}
	for {
		n, err := file.Read(buffer[:])
		if err != nil {
			log.Println(err)
			break
		}
		log.Printf("loop read file, n:%d err:%v\n", n, err)
		copy(header.Data.(protocol.MetaData).Payload, buffer[:n])
		dc.Send(header.Marshal())
	}
	<-manager.Done()
}

func generateNameFile(name string) [32]byte {
	ret := [32]byte{}
	copy(ret[:], name)
	return ret

}
