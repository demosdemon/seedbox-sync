package main

import (
	"crypto/md5"
	"fmt"
	"io"
	"os"

	jww "github.com/spf13/jwalterweatherman"
)

var _ Handler = (*localMd5sumUnit)(nil)

type localMd5sumUnit struct {
	shared       *sharedUnit
	log          *jww.Notepad
	fileUnit     *fileUnit
	fileMetadata *fileMetadata
	callback     func(error)
}

func (unit *localMd5sumUnit) Callback(err error) {
	unit.callback(err)
}

func (unit *localMd5sumUnit) Handle() {
	unit.callback(unit.simple())
}

func (unit *localMd5sumUnit) simple() error {
	cmd := fmt.Sprintf("md5sum -b %s", unit.fileMetadata.path)
	unit.log.DEBUG.Printf("local exec: %s", cmd)

	file, err := os.Open(unit.fileMetadata.path)
	if err != nil {
		unit.log.ERROR.Printf("Error opening file %s: %s", unit.fileMetadata.path, err)
		return err
	}
	defer file.Close()

	stat, err := file.Stat()
	if err != nil {
		unit.log.ERROR.Printf("Error getting file info %s: %s", unit.fileMetadata.path, err)
		return err
	}

	pb := unit.shared.NewProgressBar(
		stat.Size(),
		fmt.Sprintf("local md5sum %s", unit.fileUnit.file.Path),
	)

	hash := md5.New()
	pr := pb.ProxyReader(file)
	if _, err := io.Copy(hash, pr); err != nil {
		unit.log.ERROR.Printf("Error hashing file %s: %s", unit.fileMetadata.path, err)
		return err
	}

	unit.fileMetadata.md5sum = hash.Sum(nil)
	unit.log.TRACE.Printf("md5sum: %x", unit.fileMetadata.md5sum)
	return nil
}
