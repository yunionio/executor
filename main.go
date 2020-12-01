package main

import (
	"flag"
	"os"
	"path/filepath"
	"syscall"

	"yunion.io/x/log"
	"yunion.io/x/pkg/util/signalutils"
	"yunion.io/x/pkg/utils"
)

var isServer bool
var socketPath string

func init() {
	flag.BoolVar(&isServer, "is-server", false, "execute server")
	flag.StringVar(&socketPath, "socket-path", "/var/run/exec.sock", "execute service listen socket path")
	flag.Parse()

	var err error
	socketPath, err = filepath.Abs(socketPath)
	if err != nil {
		log.Fatalf("failed parse socket path: %s", socketPath)
	}
	err = os.MkdirAll(filepath.Dir(socketPath), 0755)
	if err != nil {
		log.Fatalf("failed mkdir socket path: %s", err)
	}

	signalutils.RegisterSignal(func() {
		log.Errorln("ALL GO ROUTINE STACK")
		utils.DumpAllGoroutineStack(log.Logger().Out)
	}, syscall.SIGUSR1)
	signalutils.StartTrap()
}

func main() {
	if isServer {
		Server()
	} else {
		Client()
	}
}
