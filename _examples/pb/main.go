package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"math/rand"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/icrowley/fake"
	"github.com/vbauerster/mpb/v8"
	"github.com/vbauerster/mpb/v8/decor"
)

var nextPriority atomic.Uint64

const (
	kilobyte = 1024
	megabyte = 1024 * kilobyte
	gigabyte = 1024 * megabyte

	kMinRandBytes      = 400 * megabyte
	kMaxRandBytes      = 40 * gigabyte
	kMinBytesPerSecond = 8 * megabyte
	kMaxBytesPerSecond = 12 * megabyte
)

func main() {
	log.SetPrefix("pb-example: ")
	log.SetFlags(log.LstdFlags | log.Lshortfile)

	var (
		wg  sync.WaitGroup
		in  = make(chan func() error)
		out = make(chan error)

		numBars     = flag.Int("num-bars", 10, "number of bars to display at once")
		totalBars   = flag.Int("total-bars", 100, "total number of bars to display")
		barMinBytes = flag.Int64("bar-min-bytes", kMinRandBytes, "minimum number of bytes per bar")
		barMaxBytes = flag.Int64("bar-max-bytes", kMaxRandBytes, "maximum number of bytes per bar")
		minBPS      = flag.Int64("min-bps", kMinBytesPerSecond, "minimum bytes per second")
		maxBPS      = flag.Int64("max-bps", kMaxBytesPerSecond, "maximum bytes per second")
	)

	flag.Parse()

	log.Printf("num-bars: %d", *numBars)
	log.Printf("total-bars: %d", *totalBars)
	log.Printf("bar-min-bytes: %d", *barMinBytes)
	log.Printf("bar-max-bytes: %d", *barMaxBytes)
	log.Printf("min-bps: %d", *minBPS)
	log.Printf("max-bps: %d", *maxBPS)

	wg.Add(*numBars)
	for i := 0; i < *numBars; i++ {
		go func() {
			defer wg.Done()
			for f := range in {
				out <- f()
			}
		}()
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		for err := range out {
			log.Printf("error: %v", err)
		}
	}()

	pb := mpb.New(
		mpb.PopCompletedMode(),
		mpb.WithAutoRefresh(),
		mpb.WithWaitGroup(&wg),
	)

	log.SetOutput(pb)

	for i := 0; i < *totalBars; i++ {
		i := i
		in <- func() error {
			log.Printf("starting bar %d", i)
			err := simulatedWorkload(pb, *barMinBytes, *barMaxBytes, *minBPS, *maxBPS)
			log.Printf("finished bar %d", i)
			return err
		}
	}

	close(in)
	wg.Wait()
}

func simulatedWorkload(
	pb *mpb.Progress,
	minRandBytes, maxRandBytes, minBytesPerSecond, maxBytesPerSecond int64,
) error {
	name := fmt.Sprintf("Combobulating the %s", fake.Product())
	if len(name) > 53 {
		name = name[:50] + "..."
	}

	pri := nextPriority.Add(1)

	randBytes := rand.Int63n(maxRandBytes-minRandBytes) + minRandBytes
	prngBuf := NewRateLimitedRandomReader(minBytesPerSecond, maxBytesPerSecond, randBytes)

	bar := pb.AddBar(
		randBytes,
		mpb.BarRemoveOnComplete(),
		mpb.BarPriority(int(pri)),
		mpb.PrependDecorators(
			decor.Name(name, decor.WCSyncWidthR),
			decor.Percentage(decor.WC{W: 6}),
		),
		mpb.AppendDecorators(
			decor.Elapsed(decor.ET_STYLE_GO, decor.WCSyncSpace),
			// "758.35 MB / 758.35 MB"
			decor.CountersKiloByte("% .1f / % .1f", decor.WCSyncSpace),
			// "259.93 MB/s"
			decor.AverageSpeed(decor.UnitKB, "% .1f", decor.WCSyncSpace),
			decor.OnComplete(
				decor.AverageETA(decor.ET_STYLE_GO, decor.WCSyncSpace),
				"done",
			),
		),
	)

	barReader := bar.ProxyReader(prngBuf)
	defer barReader.Close()

	_, err := io.Copy(io.Discard, barReader)
	return err
}

type rateLimitedRandomReader struct {
	buf   []byte
	ch    <-chan []byte
	close func()
}

func NewRateLimitedRandomReader(minBytesPerSecond, maxBytesPerSecond, totalBytes int64) io.ReadCloser {
	const tickDuration = 100 * time.Millisecond
	const ticksPerSecond = int64(time.Second / tickDuration)
	var (
		minBytesPerTick = minBytesPerSecond / ticksPerSecond
		maxBytesPerTick = maxBytesPerSecond / ticksPerSecond
	)
	ch, close := rateLimitedRandomBytes(minBytesPerTick, maxBytesPerTick, totalBytes, tickDuration)
	return &rateLimitedRandomReader{ch: ch, close: close}
}

func (r *rateLimitedRandomReader) Read(p []byte) (int, error) {
	if len(r.buf) == 0 {
		var ok bool
		r.buf, ok = <-r.ch
		if !ok {
			return 0, io.EOF
		}
	}

	n := copy(p, r.buf)
	r.buf = r.buf[n:]
	return n, nil
}

func (r *rateLimitedRandomReader) Close() error {
	r.close()
	for range r.ch {
	}
	return nil
}

func rateLimitedRandomBytes(
	minBytesPerTick, maxBytesPerTick, totalBytes int64,
	tickDuration time.Duration,
) (<-chan []byte, func()) {
	var (
		ticker = time.NewTicker(tickDuration)
		stop   = make(chan struct{})
		out    = make(chan []byte)
		rng    = rand.New(rand.NewSource(time.Now().UnixNano()))
	)

	log.Printf("minBytesPerTick: %d", minBytesPerTick)
	log.Printf("maxBytesPerTick: %d", maxBytesPerTick)
	runtime.Gosched()

	if maxBytesPerTick-minBytesPerTick <= 0 {
		log.Panicf("maxBytesPerTick must be greater than minBytesPerTick (got %d and %d)", maxBytesPerTick, minBytesPerTick)
	}

	go func() {
		defer ticker.Stop()
		defer close(out)

		for totalBytes > 0 {
			select {
			case <-stop:
				return
			case <-ticker.C:
				randBytes := rand.Int63n(maxBytesPerTick-minBytesPerTick) + minBytesPerTick
				if totalBytes < randBytes {
					randBytes = totalBytes
				}
				buf := make([]byte, randBytes)
				// read from prng never fails
				_, _ = io.ReadFull(rng, buf)
				out <- buf
				totalBytes -= randBytes
			}
		}
	}()

	return out, func() { close(stop) }
}
