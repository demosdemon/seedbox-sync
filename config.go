package main

import (
	"fmt"
	"os"
	"os/user"
	"runtime"

	"github.com/BurntSushi/toml"
	"github.com/demosdemon/seedbox-sync/lib/logging"
)

var knownFiles = []string{
	"id_rsa",
	"id_ecdsa",
	"id_ecdsa_sk",
	"id_ed25519",
	"id_ed25519_sk",
	"id_dsa",
}

type Config struct {
	Local  LocalConfig  `toml:"local"`
	Remote RemoteConfig `toml:"remote"`
}

type LocalConfig struct {
	Destination     string `toml:"destination"`
	DownloadThreads int    `toml:"download-threads,omitempty"`
	DownloadBuffer  int    `toml:"download-buffer,omitempty"`
	Md5sumThreads   int    `toml:"md5sum-threads,omitempty"`
	Md5sumBuffer    int    `toml:"md5sum-buffer,omitempty"`
}

type RemoteConfig struct {
	Md5sumThreads int            `toml:"md5sum-threads,omitempty"`
	Md5sumBuffer  int            `toml:"md5sum-buffer,omitempty"`
	Ssh           SshConfig      `toml:"ssh,omitempty"`
	Rtorrent      RtorrentConfig `toml:"rtorrent,omitempty"`
}

type SshConfig struct {
	Hostname string `toml:"hostname,omitempty"`
	Port     uint16 `toml:"port,omitempty"`
	Username string `toml:"username,omitempty"`
	KeyFile  string `toml:"keyfile,omitempty"`
}

type RtorrentConfig struct {
	Socket  string `toml:"socket,omitempty"`
	SyncTag string `toml:"sync-tag,omitempty"`
}

func (c *Config) setDefaults() error {
	if err := c.Local.setDefaults(); err != nil {
		return err
	}
	if err := c.Remote.setDefaults(); err != nil {
		return err
	}
	return nil
}

func (c *LocalConfig) setDefaults() error {
	if c.Destination == "" {
		return fmt.Errorf("local.destination must be set")
	}
	if c.DownloadThreads <= 0 {
		c.DownloadThreads = 4
	}
	if c.DownloadBuffer <= 0 {
		c.DownloadBuffer = c.DownloadThreads * 64
	}
	if c.Md5sumThreads <= 0 {
		c.Md5sumThreads = runtime.NumCPU()
	}
	if c.Md5sumBuffer <= 0 {
		c.Md5sumBuffer = c.Md5sumThreads * 64
	}
	return nil
}

func (c *RemoteConfig) setDefaults() error {
	if c.Md5sumThreads <= 0 {
		c.Md5sumThreads = 1
	}
	if c.Md5sumBuffer <= 0 {
		c.Md5sumBuffer = c.Md5sumThreads * 64
	}
	if err := c.Ssh.setDefaults(); err != nil {
		return err
	}
	if err := c.Rtorrent.setDefaults(); err != nil {
		return err
	}
	return nil
}

func (c *SshConfig) setDefaults() error {
	if c.Hostname == "" {
		return fmt.Errorf("remote.ssh.hostname must be set")
	}
	if c.Port == 0 {
		c.Port = 22
	}
	if c.Username == "" {
		user, err := user.Current()
		if err != nil {
			c.Username = "root"
		} else {
			c.Username = user.Username
		}
	}
	if c.KeyFile == "" {
		path, err := scanForPrivateKey()
		if err != nil {
			return err
		}
		c.KeyFile = path
	}
	return nil
}

func (c *RtorrentConfig) setDefaults() error {
	if c.Socket == "" {
		return fmt.Errorf("remote.rtorrent.socket must be set")
	}
	if c.SyncTag == "" {
		c.SyncTag = "sync"
	}
	return nil
}

func loadConfig(path string) (*Config, error) {
	var config Config
	if _, err := toml.DecodeFile(path, &config); err != nil {
		return nil, err
	}
	if err := config.setDefaults(); err != nil {
		return nil, err
	}
	return &config, nil
}

func scanForPrivateKey() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("error getting user home directory: %w", err)
	}

	for _, file := range knownFiles {
		path := fmt.Sprintf("%s/.ssh/%s", home, file)
		if _, err := os.Stat(path); err == nil {
			return path, nil
		}
	}

	return "", fmt.Errorf("no private key found in ~/.ssh")
}

func (c *Config) numFileHandlers() int {
	return max(max(c.Local.Md5sumThreads, c.Remote.Md5sumThreads), c.Local.DownloadThreads)
}

func (c *Config) downloadHandlers(newLog func(string) logging.Notepad) *WorkQueue[*downloadUnit] {
	return NewQueue[*downloadUnit]("download", newLog, c.Local.DownloadThreads, c.Local.DownloadBuffer)
}

func (c *Config) torrentHandlers(newLog func(string) logging.Notepad) *WorkQueue[*torrentUnit] {
	return NewQueue[*torrentUnit]("torrent", newLog, 1, 0)
}

func (c *Config) fileHandlers(newLog func(string) logging.Notepad) *WorkQueue[*fileUnit] {
	return NewQueue[*fileUnit]("file", newLog, c.numFileHandlers(), 0)
}

func (c *Config) localMd5sumHandlers(newLog func(string) logging.Notepad) *WorkQueue[*localMd5sumUnit] {
	return NewQueue[*localMd5sumUnit]("local-md5sum", newLog, c.Local.Md5sumThreads, 0)
}

func (c *Config) remoteMd5sumHandlers(newLog func(string) logging.Notepad) *WorkQueue[*remoteMd5sumUnit] {
	return NewQueue[*remoteMd5sumUnit]("remote-md5sum", newLog, c.Remote.Md5sumThreads, 0)
}
