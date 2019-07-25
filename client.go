package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"yunion.io/x/executor/client"
	"yunion.io/x/pkg/utils"
)

func Client() {
	if len(socketPath) == 0 {
		panic("socket path not provide")
	}
	client.Init(socketPath)

	start()
}

func start() {
	fmt.Print("# ")
	reader := bufio.NewReader(os.Stdin)
	var cmdRunning bool
	var Sinput io.Writer
	for {
		content, _ := reader.ReadString('\n')
		if cmdRunning {
			io.WriteString(Sinput, content)
			continue
		}
		input := strings.TrimSpace(strings.Trim(content, "\n"))
		if utils.IsInStringArray(input, []string{"exit", "quit"}) {
			return
		} else if input == "" {
			fmt.Println(time.Now().UTC().String())
			fmt.Print("# ")
			continue
		}
		is := utils.ArgsStringToArray(input)
		c := client.Command(is[0], is[1:]...)
		si, err := c.StdinPipe()
		if err != nil {
			panic(err)
		}
		Sinput = si
		stdout, err := c.StdoutPipe()
		if err != nil {
			panic(err)
		}
		stderr, err := c.StderrPipe()
		if err != nil {
			panic(err)
		}

		if err := c.Start(); err != nil {
			fmt.Printf("cmd %s start failed: %s\n", input, err)
			fmt.Print("# ")
			continue
		}

		go func() {
			io.Copy(os.Stdout, stdout)
		}()
		go func() {
			io.Copy(os.Stderr, stderr)
		}()

		cmdRunning = true
		go func() {
			if err := c.Wait(); err != nil {
				fmt.Printf("cmd %s wait failed: %s\n", input, err)
			}
			cmdRunning = false
			fmt.Print("# ")
		}()
	}
}
