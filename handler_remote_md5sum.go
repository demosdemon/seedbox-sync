package main

import (
	"bytes"
	"crypto/md5"
	"encoding/hex"
	"fmt"

	"github.com/alessio/shellescape"
	"github.com/demosdemon/seedbox-sync/lib/logging"
	"github.com/pkg/errors"
	"github.com/vbauerster/mpb/v8"
	"github.com/vbauerster/mpb/v8/decor"
)

var _ Handler = (*remoteMd5sumUnit)(nil)

type remoteMd5sumUnit struct {
	shared       *sharedUnit
	log          logging.Notepad
	fileUnit     *fileUnit
	fileMetadata *fileMetadata
	callback     func(error)
}

func (unit *remoteMd5sumUnit) Callback(err error) {
	unit.callback(err)
}

func (unit *remoteMd5sumUnit) Handle() {
	unit.callback(unit.simple())
}

func (unit *remoteMd5sumUnit) simple() error {
	cmd := fmt.Sprintf("md5sum -b %s", shellescape.Quote(unit.fileMetadata.path))
	unit.log.DEBUG.Printf("remote exec: %s", cmd)

	wc := decor.WC{W: 1, C: decor.DSyncSpace}
	pb := unit.shared.progress.New(
		0,
		mpb.SpinnerStyle().PositionLeft(),
		mpb.BarPriority(0),
		mpb.BarRemoveOnComplete(),
		mpb.PrependDecorators(
			decor.Name(fmt.Sprintf("remote md5sum %s", unit.fileUnit.file.Path)),
		),
		mpb.AppendDecorators(
			decor.OnComplete(
				decor.Elapsed(decor.ET_STYLE_GO, wc),
				"done",
			),
		),
	)
	defer pb.SetTotal(-1, true)

	sess, err := unit.shared.sshClient.NewSession()
	if err != nil {
		unit.log.ERROR.Printf("Error creating new ssh session: %s", err)
		return errors.Wrap(err, "failed to create new ssh session")
	}

	sess.Stderr = &stderrProxy{unit.shared.NewNotepad(fmt.Sprintf("%s remote md5sum stderr", unit.fileUnit.name))}
	out, err := sess.Output(cmd)
	if err != nil {
		unit.log.ERROR.Printf("Error running remote md5sum: %s", err)
		return errors.Wrap(err, "failed to run remote md5sum")
	}

	space := bytes.IndexByte(out, ' ')
	if space < 0 {
		unit.log.ERROR.Printf("remote md5sum output has no space: %q", out)
		return errors.New("remote md5sum output has no space")
	}

	md5sumStr := string(out[:space])
	md5sum, err := hex.DecodeString(md5sumStr)
	if err != nil {
		unit.log.ERROR.Printf("Error decoding remote md5sum: %s", err)
		return errors.Wrap(err, "failed to decode remote md5sum")
	}

	if len(md5sum) != md5.Size {
		unit.log.ERROR.Printf("remote md5sum has wrong length: %d != %d; %q", len(md5sum), md5.Size, md5sumStr)
		return fmt.Errorf("remote md5sum has wrong length: %d != %d", len(md5sum), md5.Size)
	}

	unit.fileMetadata.md5sum = md5sum
	unit.log.TRACE.Printf("remote md5sum: %x", md5sum)
	return nil
}

type stderrProxy struct {
	log logging.Notepad
}

func (proxy *stderrProxy) Write(p []byte) (int, error) {
	i := 0
	n := len(p)
	for i < n {
		m := bytes.Index(p[i:], []byte("\n"))
		if m < 0 {
			break
		}

		line := p[i : i+m]
		i += m + 1

		if len(line) > 0 && line[len(line)-1] == '\r' {
			line = line[:len(line)-1]
		}

		proxy.log.WARN.Print(string(line))
	}

	if i < n {
		proxy.log.WARN.Print(string(p[i:]))
	}

	return n, nil
}
