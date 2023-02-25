package main

import (
	"net"
	"net/http"

	"github.com/mrobinsn/go-rtorrent/rtorrent"
	jww "github.com/spf13/jwalterweatherman"
	"golang.org/x/crypto/ssh"
)

func (c *Config) RTorrentClient(log *jww.Notepad, ssh *ssh.Client) *rtorrent.RTorrent {
	httpClient := &http.Client{
		Transport: scgiProxy{
			dial: func() (net.Conn, error) {
				log.TRACE.Printf("Connecting to %s via SSH", c.Remote.Rtorrent.Socket)
				return ssh.Dial("unix", c.Remote.Rtorrent.Socket)
			},
		},
	}

	return rtorrent.New("", false).WithHTTPClient(httpClient)
}
