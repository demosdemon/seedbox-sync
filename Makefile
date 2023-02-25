seedbox-sync: $(shell find . -name '*.go') go.mod go.sum
	CGO_ENABLED=0 go build -ldflags="-extldflags=-static" -o $@
	strip $@
