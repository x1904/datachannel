package datachannel

import (
	"context"
	"errors"
	"log"
	"net/http"
)

type Signaling struct {
	ctx      context.Context
	provider *WebrtcDataChannel
	address  string
	routes   map[string]func(w http.ResponseWriter, _ *http.Request)
}

func NewSignaling(ctx context.Context, config *SignalingConfig) (*Signaling, error) {
	if config == nil {
		return nil, errors.New("Invalid config")
	}
	s := Signaling{
		ctx:      ctx,
		provider: config.Provider,
		routes:   config.Routes,
		address:  config.Address,
	}

	return &s, nil
}

func (s *Signaling) Start() error {
	err := s.ctx.Err()
	if err != nil {
		return err
	}

	go func() {
		for route, handle := range s.routes {
			log.Printf("New route '%s' -> %p\n", route, handle)
			http.HandleFunc(route, handle)
		}

		log.Println("Server HTTP started:", s.address)
		log.Fatal(http.ListenAndServe(s.address, nil))
	}()
	return nil
}

func (s *Signaling) Stop() error {
	err := s.ctx.Err()
	if err != nil {
		return err
	}

	log.Println("Signaling stopped")
	return nil
}

func replyStatus(rw http.ResponseWriter, status int) {
	if rw != nil {
		rw.WriteHeader(status)
	}
}
