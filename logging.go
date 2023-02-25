package main

import (
	"io"
	"log"

	jww "github.com/spf13/jwalterweatherman"
)

func NewNotepad(stdio, file io.Writer, prefix string) *jww.Notepad {
	return jww.NewNotepad(
		jww.LevelDebug,
		jww.LevelTrace,
		stdio,
		file,
		prefix,
		log.Ldate|log.Ltime|log.Lshortfile,
	)
}
