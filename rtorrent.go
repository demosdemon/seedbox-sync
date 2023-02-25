package main

import (
	"net"
	"net/http"

	"github.com/demosdemon/seedbox-sync/lib/logging"
	"github.com/mrobinsn/go-rtorrent/rtorrent"
	"golang.org/x/crypto/ssh"
)

func (c *Config) RTorrentClient(log logging.Notepad, ssh *ssh.Client) *rtorrent.RTorrent {
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
