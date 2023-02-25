package logging

import (
	"io"
	"log"

	jww "github.com/spf13/jwalterweatherman"
)

type Notepad = *jww.Notepad

func New(stdio, file io.Writer, prefix string) Notepad {
	return jww.NewNotepad(
		jww.LevelDebug,
		jww.LevelTrace,
		stdio,
		file,
		prefix,
		log.Ldate|log.Ltime|log.Lshortfile,
	)
}
