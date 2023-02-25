package main

import (
	"errors"
	"fmt"

	"github.com/mrobinsn/go-rtorrent/rtorrent"
	jww "github.com/spf13/jwalterweatherman"
)

var _ Handler = (*torrentUnit)(nil)

type torrentUnit struct {
	shared   *sharedUnit
	log      *jww.Notepad
	name     string
	torrent  rtorrent.Torrent
	index    int
	callback func(error)
}

func (unit *torrentUnit) Callback(err error) {
	unit.callback(err)
}

func (unit *torrentUnit) Handle() {
	if !unit.torrent.Completed {
		unit.log.INFO.Println("skipping torrent as it has not yet completed")
		unit.callback(nil)
		return
	}

	if unit.torrent.Label == unit.shared.config.Remote.Rtorrent.SyncTag {
		unit.log.INFO.Println("skipping torrent as it is labeled as synced")
		unit.callback(nil)
		return
	}

	unit.log.INFO.Println("listing files...")
	files, err := unit.shared.rtorrentClient.GetFiles(unit.torrent)
	if err != nil {
		unit.log.ERROR.Printf("failed to list files: %s", err)
		unit.callback(err)
		return
	}

	fileErrors := make(chan error)

	unit.log.INFO.Printf("found %d file(s)...", len(files))
	manyFiles := len(files) > 1

	for idx, file := range files {
		var name string
		if manyFiles {
			name = fmt.Sprintf("%s File %s", unit.name, file.Path)
		} else {
			name = fmt.Sprintf("File %s", file.Path)
		}
		next := &fileUnit{
			shared:      unit.shared,
			log:         unit.shared.NewNotepad(name),
			name:        name,
			torrentUnit: unit,
			manyFiles:   manyFiles,
			file:        file,
			index:       idx,
			callback: func(err error) {
				fileErrors <- err
			},
		}
		unit.shared.fileHandler.Send(next)
	}

	// all files have been enqueued, release the torrent worker and wait for the results
	go func() {
		var err error
		defer func() {
			if r := recover(); r != nil {
				unit.log.CRITICAL.Printf("panic: %s", r)
				err = fmt.Errorf("panic: %s", r)
			}

			unit.callback(err)
		}()

		fileErrorsArr, err := ExactChannel(fileErrors, len(files))
		close(fileErrors)
		err = errors.Join(append(fileErrorsArr, err)...)

		if err != nil {
			unit.log.ERROR.Printf("failed to process all files: %s", err)
			return
		}

		unit.log.INFO.Println("all files processed")

		if *flagDryRun {
			unit.log.INFO.Println("dry-run enabled, skipping update of label")
			return
		}

		err = unit.shared.rtorrentClient.SetLabel(unit.torrent, unit.shared.config.Remote.Rtorrent.SyncTag)
		if err != nil {
			unit.log.ERROR.Printf("failed to set label: %s", err)
		}
	}()
}
