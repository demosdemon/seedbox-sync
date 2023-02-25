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
	fileErrors, nFiles, err := func() (chan error, int, error) {
		if !unit.torrent.Completed {
			unit.log.INFO.Println("skipping torrent as it has not yet completed")
			return nil, 0, nil
		}

		if unit.torrent.Label == unit.shared.config.Remote.Rtorrent.SyncTag {
			unit.log.INFO.Println("skipping torrent as it is labeled as synced")
			return nil, 0, nil
		}

		unit.log.INFO.Println("listing files...")
		files, err := unit.shared.rtorrentClient.GetFiles(unit.torrent)
		if err != nil {
			unit.log.ERROR.Printf("failed to list files: %s", err)
			unit.callback(err)
			return nil, 0, err
		}

		// buffer the errors so that we do not deadlock if we are blocked on pushing files to the file handler
		nFiles := len(files)
		fileErrors := make(chan error, nFiles)

		unit.log.INFO.Printf("found %d file(s)...", nFiles)
		manyFiles := nFiles > 1

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

		return fileErrors, nFiles, nil
	}()

	if err != nil {
		unit.callback(err)
		return
	}

	if fileErrors == nil {
		unit.callback(nil)
		return
	}

	unit.log.DEBUG.Println("waiting for all files to be processed...")
	go func() {
		var err error
		defer func() {
			if r := recover(); r != nil {
				err = fmt.Errorf("recovered from panic: %v", r)
			}

			unit.callback(err)
		}()

		err = func() error {
			fileErrorsArr, err := ExactChannel(fileErrors, nFiles)
			close(fileErrors)
			err = errors.Join(append(fileErrorsArr, err)...)
			if err != nil {
				unit.log.ERROR.Printf("failed to process all files: %s", err)
				return err
			}

			unit.log.INFO.Println("all files processed")
			if *flagDryRun {
				unit.log.INFO.Println("dry-run enabled, skipping update of label")
				return err
			}

			unit.log.INFO.Println("updating label...")
			err = unit.shared.rtorrentClient.SetLabel(unit.torrent, unit.shared.config.Remote.Rtorrent.SyncTag)
			if err != nil {
				unit.log.ERROR.Printf("failed to set label: %s", err)
				return err
			}

			return nil
		}()
	}()
}
