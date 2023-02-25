package pool

import (
	"sync"
	"time"
)

const maxDuration time.Duration = 1<<63 - 1

type Printer interface {
	Print(...any)
	Printf(string, ...any)
	Println(...any)
}

type Pool[T any] struct {
	wg1          *sync.WaitGroup // waits for the 3 channels to drain
	wg2          *sync.WaitGroup // waits for the worker goroutine to exit
	closeReq     chan<- struct{}
	newItemReq   chan<- request[T]
	putItemReq   chan<- T
	updateConfig chan<- Option[T]
}

func NewPool[T any](newItem func(Printer) (T, error), opts ...Option[T]) *Pool[T] {
	var state poolState[T]
	state.newItem = newItem
	for _, opt := range opts {
		opt(&state)
	}

	if state.newItem == nil {
		panic("newItem must not be nil")
	}

	var (
		wg1          sync.WaitGroup
		wg2          sync.WaitGroup
		closeReq     = make(chan struct{})
		newItemReq   = make(chan request[T])
		putItemReq   = make(chan T)
		updateConfig = make(chan Option[T])
	)

	wg1.Add(3)
	wg2.Add(1)
	go func() {
		defer wg2.Done()

		timer := time.NewTimer(maxDuration)
		defer timer.Stop()

		for {
			state.drainExpiredItems()
			sleep := state.sleepTime()

			if !timer.Stop() {
				<-timer.C
			}
			timer.Reset(sleep)

			select {
			case <-closeReq:
				return

			case req, ok := <-newItemReq:
				if !ok {
					wg1.Done()
					newItemReq = nil
					continue
				}

				item, err := state.getNewItem(req.log)
				req.ch <- result[T]{item, err}

			case item, ok := <-putItemReq:
				if !ok {
					wg1.Done()
					putItemReq = nil
					continue
				}

				// excess items will be dropped at the top of the loop
				state.items = append(state.items, idle[T]{item, time.Now()})

			case opt, ok := <-updateConfig:
				if !ok {
					wg1.Done()
					updateConfig = nil
					continue
				}

				opt(&state)

			case <-timer.C:
				// must reset the timer now so the stop/drain logic works without deadlock
				timer.Reset(maxDuration)
			}
		}
	}()

	return &Pool[T]{
		wg1:          &wg1,
		wg2:          &wg2,
		closeReq:     closeReq,
		newItemReq:   newItemReq,
		putItemReq:   putItemReq,
		updateConfig: updateConfig,
	}
}

func (p *Pool[T]) Close() {
	p.UpdateConfig(
		func(state *poolState[T]) {
			state.Print("closing pool")
			state.maxIdle = 0
		},
	)
	close(p.updateConfig)
	close(p.putItemReq)
	close(p.newItemReq)
	p.wg1.Wait()
	close(p.closeReq)
	p.wg2.Wait()
}

func (p *Pool[T]) Get(log Printer) (T, error) {
	ch := make(chan result[T])
	p.newItemReq <- request[T]{log, ch}
	res := <-ch
	return res.item, res.err
}

func (p *Pool[T]) Put(item T) {
	p.putItemReq <- item
}

func (p *Pool[T]) UpdateConfig(opt ...Option[T]) {
	for _, o := range opt {
		p.updateConfig <- o
	}
}

type result[T any] struct {
	item T
	err  error
}

type request[T any] struct {
	log Printer
	ch  chan<- result[T]
}

type idle[T any] struct {
	item T
	time time.Time
}

type poolState[T any] struct {
	newItem     func(Printer) (T, error)
	refreshItem func(Printer, T) error
	dropItem    func(T)
	maxIdle     int
	maxIdleTime time.Duration
	debug       Printer

	items []idle[T]
}

func (state *poolState[T]) Print(v ...any) {
	if state.debug != nil {
		state.debug.Print(v...)
	}
}

func (state *poolState[T]) Printf(format string, v ...any) {
	if state.debug != nil {
		state.debug.Printf(format, v...)
	}
}

func (state *poolState[T]) Println(v ...any) {
	if state.debug != nil {
		state.debug.Println(v...)
	}
}

func (state *poolState[T]) drainExpiredItems() {
	for len(state.items) > 0 {
		drop := len(state.items) > state.maxIdle
		drop = drop || (state.maxIdleTime > 0 && time.Since(state.items[0].time) > state.maxIdleTime)
		if !drop {
			break
		}

		state.Printf("dropping expired item: %v", state.items[0].item)

		if state.dropItem != nil {
			state.dropItem(state.items[0].item)
		}

		state.items = state.items[1:]
	}
}

func (state *poolState[T]) sleepTime() time.Duration {
	if state.maxIdleTime <= 0 {
		return maxDuration
	}

	if len(state.items) == 0 {
		return maxDuration
	}

	dur := state.maxIdleTime - time.Since(state.items[0].time)
	if dur < 0 {
		dur = 0
	}
	return dur
}

func (state *poolState[T]) getNewItem(log Printer) (T, error) {
	if len(state.items) > 0 {
		item := state.items[0].item
		state.items = state.items[1:]
		if state.refreshItem != nil {
			state.Printf("refreshing item: %v", item)
			return item, state.refreshItem(log, item)
		} else {
			state.Println("reusing item")
			return item, nil
		}
	}

	state.Println("creating new item")
	return state.newItem(log)
}

type Option[T any] func(*poolState[T])

func OptionNewItem[T any](f func(Printer) (T, error)) Option[T] {
	return func(state *poolState[T]) {
		state.newItem = f
	}
}

func OptionRefreshItem[T any](f func(Printer, T) error) Option[T] {
	return func(state *poolState[T]) {
		state.refreshItem = f
	}
}

func OptionDropItem[T any](f func(T)) Option[T] {
	return func(state *poolState[T]) {
		state.dropItem = f
	}
}

func OptionMaxIdle[T any](n int) Option[T] {
	return func(state *poolState[T]) {
		state.maxIdle = n
	}
}

func OptionMaxIdleTime[T any](d time.Duration) Option[T] {
	return func(state *poolState[T]) {
		state.maxIdleTime = d
	}
}

func OptionDebug[T any](log Printer) Option[T] {
	return func(state *poolState[T]) {
		state.debug = log
	}
}
