package main

import "flag"

var isServer bool
var socketPath string

func init() {
	flag.BoolVar(&isServer, "is-server", false, "execute server")
	flag.StringVar(&socketPath, "socket-path", "/var/run/exec.sock", "execute service listen socket path")
	flag.Parse()
}

func main() {
	if isServer {
		Server()
	} else {
		Client()
	}
}
