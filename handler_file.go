package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path"

	"github.com/demosdemon/seedbox-sync/lib/logging"
	"github.com/mrobinsn/go-rtorrent/rtorrent"
)

var _ Handler = (*fileUnit)(nil)

type fileUnit struct {
	shared      *sharedUnit
	log         logging.Notepad
	name        string
	torrentUnit *torrentUnit
	manyFiles   bool
	file        rtorrent.File
	index       int
	callback    func(error)
}

type fileMetadata struct {
	path   string
	size   uint64
	exists bool
	md5sum []byte
}

func (unit *fileUnit) statRemote() (fileMetadata, error) {
	var metadata fileMetadata
	metadata.path = path.Join(unit.torrentUnit.torrent.Path, unit.file.Path)
	metadata.size = uint64(unit.file.Size)

	unit.log.DEBUG.Printf("statRemote(%s)", metadata.path)
	stat, err := unit.shared.sftpClient.Stat(metadata.path)
	if err != nil {
		unit.log.ERROR.Printf("remote: failed to stat remote file: %s", err)
		return metadata, err
	}

	metadata.exists = true

	if uint64(stat.Size()) != metadata.size {
		unit.log.ERROR.Printf("remote: size mismatch: %d != %d", stat.Size(), metadata.size)
		err = fmt.Errorf("remote: %s size mismatch: %d != %d", metadata.path, stat.Size(), metadata.size)
	}

	return metadata, err
}

func (unit *fileUnit) statLocal() (fileMetadata, error) {
	var metadata fileMetadata
	if unit.manyFiles {
		metadata.path = path.Join(unit.shared.config.Local.Destination, unit.torrentUnit.torrent.Name, unit.file.Path)
	} else {
		metadata.path = path.Join(unit.shared.config.Local.Destination, unit.file.Path)
	}
	unit.log.DEBUG.Printf("statLocal(%s)", metadata.path)
	stat, err := os.Stat(metadata.path)
	if err == nil {
		metadata.exists = true
		metadata.size = uint64(stat.Size())
	}
	if err != nil && os.IsNotExist(err) {
		err = nil
	}
	if err != nil {
		unit.log.ERROR.Printf("local: failed to stat local file: %s", err)
	}
	return metadata, err
}

func (unit *fileUnit) doDownload(remote, local fileMetadata) {
	unit.shared.downloadHandler.Send(&downloadUnit{
		shared:   unit.shared,
		log:      unit.shared.NewNotepad(fmt.Sprintf("%s download", unit.name)),
		fileUnit: unit,
		local:    local,
		remote:   remote,
		callback: unit.callback,
	})
}

func (unit *fileUnit) Callback(err error) {
	unit.callback(err)
}

func (unit *fileUnit) Handle() {
	rstat, err := unit.statRemote()
	if err != nil {
		unit.callback(err)
		return
	}

	lstat, err := unit.statLocal()
	if err != nil {
		unit.callback(err)
		return
	}

	if !lstat.exists {
		unit.log.INFO.Printf("Local file %s does not exist, downloading", lstat.path)
		unit.doDownload(rstat, lstat)
		return
	}

	if lstat.size != rstat.size {
		unit.log.INFO.Printf("Local file %s size mismatch, downloading", lstat.path)
		unit.doDownload(rstat, lstat)
		return
	}

	errCh := make(chan error)

	unit.shared.localMd5sumHandler.Send(&localMd5sumUnit{
		shared:       unit.shared,
		log:          unit.shared.NewNotepad(fmt.Sprintf("%s local md5sum", unit.name)),
		fileUnit:     unit,
		fileMetadata: &lstat,
		callback: func(err error) {
			errCh <- err
		},
	})

	unit.shared.remoteMd5sumHandler.Send(&remoteMd5sumUnit{
		shared:       unit.shared,
		log:          unit.shared.NewNotepad(fmt.Sprintf("%s remote md5sum", unit.name)),
		fileUnit:     unit,
		fileMetadata: &rstat,
		callback: func(err error) {
			errCh <- err
		},
	})

	unit.log.DEBUG.Println("waiting for md5sum values")
	go func() {
		errArr, err := ExactChannel(errCh, 2)
		err = errors.Join(append(errArr, err)...)

		if err != nil {
			unit.log.ERROR.Printf("error getting md5sums: %s", err)
			unit.callback(err)
			return
		}

		if bytes.Equal(lstat.md5sum, rstat.md5sum) {
			unit.log.INFO.Println("local file md5sum matches remote")
			unit.callback(nil)
			return
		}

		unit.log.INFO.Println("local file md5sum mismatch, downloading")
		unit.doDownload(rstat, lstat)
	}()
}
