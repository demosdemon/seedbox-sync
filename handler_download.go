package main

import (
	"fmt"
	"io"
	"os"
	"path"

	"github.com/pkg/errors"
	jww "github.com/spf13/jwalterweatherman"
	"github.com/vbauerster/mpb/v8"
	"github.com/vbauerster/mpb/v8/decor"
)

var _ Handler = (*downloadUnit)(nil)

type downloadUnit struct {
	shared   *sharedUnit
	log      *jww.Notepad
	fileUnit *fileUnit
	local    fileMetadata
	remote   fileMetadata
	callback func(error)
}

func (unit *downloadUnit) Callback(err error) {
	unit.callback(err)
}

func (unit *downloadUnit) Handle() {
	unit.callback(unit.simple())
}

func (unit *downloadUnit) simple() error {
	unit.log.INFO.Printf("downloading %s to %s", unit.remote.path, unit.local.path)
	if *flagDryRun {
		unit.log.WARN.Println("dry run: skipping download")
		return nil
	}

	// we are dialing a new ssh connection here so that
	// a) we do not block the main ssh connection
	// b) we get better throughput

	conn, err := unit.shared.sftpClientPool.Get(unit.log.DEBUG)
	if err != nil {
		unit.log.ERROR.Printf("failed to dial ssh connection: %s", err)
		return errors.Wrap(err, "failed to dial ssh connection")
	}
	defer unit.shared.sftpClientPool.Put(conn)

	parent := path.Dir(unit.local.path)
	if err := os.MkdirAll(parent, 0755); err != nil {
		unit.log.ERROR.Printf("failed to create parent directory %q: %s", parent, err)
		return errors.Wrap(err, "failed to create parent directory")
	}

	localFile, err := os.Create(unit.local.path)
	if err != nil {
		unit.log.ERROR.Printf("failed to create local file %q: %s", unit.local.path, err)
		return errors.Wrap(err, "failed to create local file")
	}
	defer localFile.Close()

	remoteFile, err := conn.sftpClient.Open(unit.remote.path)
	if err != nil {
		unit.log.ERROR.Printf("failed to open remote file %q: %s", unit.remote.path, err)
		return errors.Wrap(err, "failed to open remote file")
	}
	defer remoteFile.Close()

	name := fmt.Sprintf("downloading %s", unit.fileUnit.file.Path)
	wc := decor.WC{W: 2, C: decor.DSyncSpace}
	pb := unit.shared.progress.AddBar(
		int64(unit.remote.size),
		mpb.BarRemoveOnComplete(),
		mpb.BarPriority(int(unit.shared.nextPriority.Add(1))),
		mpb.PrependDecorators(
			decor.Name(name),
			decor.Percentage(decor.WCSyncSpace),
		),
		mpb.AppendDecorators(
			decor.OnComplete(
				decor.Elapsed(decor.ET_STYLE_GO, wc),
				"done",
			),
			decor.CountersKiloByte("% .2f / % .2f", wc),
			decor.EwmaSpeed(decor.UnitKB, "% 3.2f", 120, wc),
			decor.EwmaETA(decor.ET_STYLE_GO, 120, wc),
		),
	)

	pw := pb.ProxyWriter(localFile)
	defer pw.Close()

	_, err = io.Copy(pw, remoteFile)
	if err != nil {
		unit.log.ERROR.Printf("failed to copy remote file %q to local file %q: %s", unit.remote.path, unit.local.path, err)
	}

	return err
}
