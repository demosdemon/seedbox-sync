package main

import (
	"fmt"
	"os"

	"github.com/demosdemon/seedbox-sync/lib/pool"
	"github.com/pkg/errors"
	"golang.org/x/crypto/ssh"
)

func (c *Config) DialSSH(log pool.Printer) (*ssh.Client, error) {
	privateKey, err := os.ReadFile(c.Remote.Ssh.KeyFile)
	if err != nil {
		return nil, errors.Wrap(err, "ssh: error reading private key")
	}

	signer, err := ssh.ParsePrivateKey(privateKey)
	if err != nil {
		return nil, errors.Wrap(err, "ssh: error parsing private key")
	}

	auth := ssh.PublicKeys(signer)

	sshConfig := ssh.ClientConfig{
		User: c.Remote.Ssh.Username,
		Auth: []ssh.AuthMethod{auth},
		// TODO: This is insecure, but I don't care for now.
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		BannerCallback:  ssh.BannerDisplayStderr(),
	}

	addr := fmt.Sprintf("%s:%d", c.Remote.Ssh.Hostname, c.Remote.Ssh.Port)

	log.Printf("Connecting to %s", addr)
	return ssh.Dial("tcp", addr, &sshConfig)
}
