package main

import (
	"io"
	"os"
	"sync/atomic"
	"time"

	"github.com/demosdemon/seedbox-sync/lib/pool"
	"github.com/mrobinsn/go-rtorrent/rtorrent"
	"github.com/pkg/sftp"
	jww "github.com/spf13/jwalterweatherman"
	"github.com/vbauerster/mpb/v8"
	"github.com/vbauerster/mpb/v8/decor"
	"golang.org/x/crypto/ssh"
)

type sharedUnit struct {
	nextPriority        atomic.Uint64
	progress            *mpb.Progress
	fileLogger          io.Writer
	log                 *jww.Notepad
	config              *Config
	sftpClientPool      *pool.Pool[*pooledSftpClient]
	sshClient           *ssh.Client
	sftpClient          *sftp.Client
	rtorrentClient      *rtorrent.RTorrent
	downloadHandler     *WorkQueue[*downloadUnit]
	localMd5sumHandler  *WorkQueue[*localMd5sumUnit]
	remoteMd5sumHandler *WorkQueue[*remoteMd5sumUnit]
	fileHandler         *WorkQueue[*fileUnit]
	torrentHandler      *WorkQueue[*torrentUnit]
}

func (unit *sharedUnit) NewNotepad(prefix string) *jww.Notepad {
	return NewNotepad(unit.progress, unit.fileLogger, prefix)
}

func (unit *sharedUnit) NewProgressBar(total int64, name string, options ...mpb.BarOption) *mpb.Bar {
	if len(name) > 33 {
		name = name[:30] + "..."
	}

	opts := []mpb.BarOption{
		mpb.BarRemoveOnComplete(),
		mpb.BarPriority(int(unit.nextPriority.Add(1))),
		mpb.PrependDecorators(
			decor.Name(name, decor.WCSyncWidth),
			decor.Percentage(decor.WCSyncSpace),
		),
		mpb.AppendDecorators(
			decor.OnComplete(
				decor.Elapsed(decor.ET_STYLE_GO, decor.WC{W: 3, C: decor.DSyncSpaceR}),
				"done",
			),
			decor.CountersKiloByte("% .2f / % .2f", decor.WC{W: 19, C: decor.DSyncSpaceR}),
			decor.AverageSpeed(decor.UnitKB, "% 3.2f", decor.WC{W: 11, C: decor.DSyncSpaceR}),
			decor.AverageETA(decor.ET_STYLE_GO, decor.WC{W: 3, C: decor.DSyncSpaceR}),
		),
	}
	return unit.progress.AddBar(total, append(opts, options...)...)
}

func (unit *sharedUnit) Close() {
	unit.torrentHandler.Close()
	unit.fileHandler.Close()
	unit.remoteMd5sumHandler.Close()
	unit.localMd5sumHandler.Close()
	unit.downloadHandler.Close()
	unit.log.DEBUG.Println("Closing sshClient")
	unit.sshClient.Close()
	unit.progress.Wait()
	if w, ok := unit.fileLogger.(*os.File); ok {
		w.Close()
	}
}

func NewSharedUnit(configPath string) *sharedUnit {
	var shared sharedUnit
	var err error

	shared.nextPriority.Store(3)

	// Progress writer must be configured before any logging output is generated
	shared.progress = mpb.New(
		mpb.PopCompletedMode(),
		mpb.WithAutoRefresh(),
	)

	writer := true
	shared.fileLogger, err = os.OpenFile("seedbox-sync.log", os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		shared.fileLogger = io.Discard
		writer = false
	}

	// The logger must be configured immediately after the progress writer
	shared.log = shared.NewNotepad("seedbox-sync")
	if !writer {
		shared.log.WARN.Printf("Unable to open log file for writing: %s", err)
	}

	shared.config, err = loadConfig(configPath)
	if err != nil {
		shared.log.FATAL.Panicf("Error loading config: %s", err)
	}

	shared.sftpClientPool = pool.NewPool(
		func(log pool.Printer) (*pooledSftpClient, error) {
			return newPooledSftpClient(shared.config, log)
		},
		pool.OptionDropItem(func(c *pooledSftpClient) {
			c.sshClient.Close()
		}),
		pool.OptionMaxIdle[*pooledSftpClient](shared.config.Local.DownloadThreads),
		pool.OptionMaxIdleTime[*pooledSftpClient](time.Minute),
		pool.OptionDebug[*pooledSftpClient](shared.log.TRACE),
	)

	conn, err := shared.sftpClientPool.Get(shared.log.DEBUG)
	if err != nil {
		shared.log.FATAL.Panicf("Error connecting to ssh: %s", err)
	}

	shared.sshClient = conn.sshClient
	shared.sftpClient = conn.sftpClient

	shared.rtorrentClient = shared.config.RTorrentClient(shared.NewNotepad("rtorrent"), shared.sshClient)
	shared.downloadHandler = shared.config.downloadHandlers(shared.NewNotepad)
	shared.localMd5sumHandler = shared.config.localMd5sumHandlers(shared.NewNotepad)
	shared.remoteMd5sumHandler = shared.config.remoteMd5sumHandlers(shared.NewNotepad)
	shared.fileHandler = shared.config.fileHandlers(shared.NewNotepad)
	shared.torrentHandler = shared.config.torrentHandlers(shared.NewNotepad)

	return &shared
}

type pooledSftpClient struct {
	sshClient  *ssh.Client
	sftpClient *sftp.Client
}

func newPooledSftpClient(config *Config, log pool.Printer) (*pooledSftpClient, error) {
	sshClient, err := config.DialSSH(log)
	if err != nil {
		return nil, err
	}

	sftpClient, err := sftp.NewClient(sshClient)
	if err != nil {
		sshClient.Close()
		return nil, err
	}

	return &pooledSftpClient{
		sshClient:  sshClient,
		sftpClient: sftpClient,
	}, nil

}
