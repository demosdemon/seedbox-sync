package main

import (
	"flag"
	"fmt"
	"sort"

	"github.com/mrobinsn/go-rtorrent/rtorrent"
)

func main() {
	flag.Parse()

	shared := NewSharedUnit("config.toml")
	defer shared.Close()

	shared.log.INFO.Println("Getting torrents...")
	torrents, err := shared.rtorrentClient.GetTorrents(rtorrent.ViewMain)
	if err != nil {
		shared.log.FATAL.Panicf("Error getting torrents: %s", err)
	}

	shared.log.INFO.Printf("Fetched %d torrents", len(torrents))

	sort.Slice(torrents, func(i, j int) bool {
		a := torrents[i].Finished
		b := torrents[j].Finished
		return a.Before(b)
	})

	for idx, torrent := range torrents {
		name := fmt.Sprintf("Torrent %s", torrent.Name)
		shared.torrentHandler.Send(&torrentUnit{
			shared:  shared,
			log:     shared.NewNotepad(name),
			name:    name,
			torrent: torrent,
			index:   idx,
			callback: func(err error) {
				if err != nil {
					shared.log.ERROR.Printf("Error processing torrent %s: %s", name, err)
				}
			},
		})
	}
}

func max(a, b int) int {
	if a < b {
		return b
	}
	return a
}
