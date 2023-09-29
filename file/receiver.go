package file

import (
	"context"
	"sync"

	"github.com/x1904/datachannel/protocol"
)

type dispatcher struct {
	wg      *sync.WaitGroup
	workers map[string]worker
	chTasks chan protocol.MetaData
	ctx     context.Context
}

func newDispatcher() *dispatcher {
	d := dispatcher{
		workers: map[string]worker,
	}
}

func (d *dispatcher) start(ctx context.Context) {
	d.chTasks = make(protocol.MetaData)
	d.ctx = ctx

	go func() {
		defer close(d.chTasks)

		for {
			select {
			case <-ctx.Done():
				return
			case data := <-d.chTasks:

			}
		}
	}()
}

func (d *dispatcher) handle(data protocol.MetaData) {
	if d.chTasks == nil {
		return
	}

	d.chTasks <- data
}

type Receiver struct {
	dispatcher dispatcher
}

func (r *Receiver) Read([]byte) (n int, err error) {
	n = 0
	err = nil

	return
}
