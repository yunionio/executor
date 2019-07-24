package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"
	"syscall"

	"yunion.io/x/executor/client"
	"yunion.io/x/log"
	"yunion.io/x/pkg/util/signalutils"
	"yunion.io/x/pkg/utils"
)

func Client() {
	if len(socketPath) == 0 {
		panic("socket path not provide")
	}
	client.Init(socketPath)

	signalutils.RegisterSignal(func() {
		utils.DumpAllGoroutineStack(log.Logger().Out)
	}, syscall.SIGUSR1)
	signalutils.StartTrap()

	// testPipeLine()
	fmt.Print("# ")
	reader := bufio.NewReader(os.Stdin)
	var cmdRunning bool
	var pr *io.PipeReader
	var pw *io.PipeWriter
	for {
		content, _ := reader.ReadString('\n')
		if cmdRunning {
			io.WriteString(pw, content)
			continue
		}
		input := strings.TrimSpace(strings.Trim(content, "\n"))
		if utils.IsInStringArray(input, []string{"exit", "quit"}) {
			return
		} else if input == "" {
			fmt.Print("# ")
			continue
		}
		// inputCmd := utils.ArgsStringToArray(input)
		pr, pw = io.Pipe()
		// ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		c := client.Command("sh", "-c", input)
		c.Stdin = pr
		c.Stdout = os.Stdout
		c.Stderr = os.Stderr

		if err := c.Start(); err != nil {
			fmt.Printf("cmd %s start failed: %s\n", input, err)
			continue
		}
		cmdRunning = true
		go func() {
			if err := c.Wait(); err != nil {
				fmt.Printf("cmd %s wait failed: %s\n", input, err)
			}
			// cancel()
			pr.Close()
			cmdRunning = false
			fmt.Print("# ")
		}()
	}
}
