package main

import (
	"fmt"
	"sync"

	jww "github.com/spf13/jwalterweatherman"
)

type Handler interface {
	Handle()

	Callback(error)
}

type WorkQueue[T Handler] struct {
	log *jww.Notepad
	wg  sync.WaitGroup
	ch  chan<- T
}

type worker[T Handler] struct {
	log *jww.Notepad
	wg  *sync.WaitGroup
	ch  <-chan T
}

func (w worker[T]) exec() {
	defer func() {
		w.log.DEBUG.Println("worker exited")
		w.wg.Done()
	}()

	w.log.DEBUG.Println("worker started...")

	for unit := range w.ch {
		func() {
			defer func() {
				if r := recover(); r != nil {
					w.log.ERROR.Printf("recovered from panic: %v", r)
					func() {
						defer func() {
							if r := recover(); r != nil {
								w.log.CRITICAL.Printf("recovered from panic: %v", r)
							}
						}()

						unit.Callback(fmt.Errorf("recovered from panic: %v", r))
					}()
				}
			}()

			w.log.TRACE.Printf("handling %T", unit)
			unit.Handle()
		}()
	}
}

func (queue *WorkQueue[T]) Send(unit T) {
	queue.log.TRACE.Printf("sending %T", unit)
	queue.ch <- unit
	queue.log.TRACE.Printf("sent %T", unit)
}

func (queue *WorkQueue[T]) Close() {
	queue.log.DEBUG.Println("closing handler")
	close(queue.ch)
	queue.log.DEBUG.Println("waiting for handler to exit")
	queue.wg.Wait()
	queue.log.DEBUG.Println("handler exited")
}

func NewQueue[T Handler](name string, newLog func(string) *jww.Notepad, count, buffer int) *WorkQueue[T] {
	var ch chan T
	if buffer > 0 {
		ch = make(chan T, buffer)
	} else {
		ch = make(chan T)
	}

	queue := &WorkQueue[T]{
		log: newLog(fmt.Sprintf("%s-queue", name)),
		ch:  ch,
	}

	queue.wg.Add(count)
	for idx := 0; idx < count; idx++ {
		go worker[T]{
			wg:  &queue.wg,
			log: newLog(fmt.Sprintf("%s-worker-%d", name, idx)),
			ch:  ch,
		}.exec()
	}

	return queue
}
