remote cmds exec tool base on grpc

## new executor server
```go
    grpcServer := grpc.NewServer()
    // register server.Executor
    apis.RegisterExecutorServer(grpcServer, &server.Executor{})
    // new tcp server
    listener, err := net.Listen("unix", socketPath)
    if err != nil {
        log.Fatalln(err)
    }
    defer listener.Close()
    err = grpcServer.Serve(listener)
    if err != nil {
        log.Fatalln(err)
    }
```

## executor client
same interface with os/exec: `Run`, `Start`, `Wait`, `Output`, `CombinedOutput`
`StdinPipe`, `StdoutPipe`, `StderrPipe`
```go
    client.Command("pwd").Run()
    client.Command("pwd").Output()
    client.Command("pwd").CombinedOutput()
```
different error handle
- get exit code
```go
func GetExitStatus(err error) (int, bool) {
    if exiterr, ok := err.(*client.ExitError); ok {
        ws := exiterr.Sys().(syscall.WaitStatus)
        return ws.ExitStatus(), true
    } else {
        return 0, false
    }
}
err := client.Command("pwd").Run()
exitStatus, ok := GetExitStatus(err)
```
- kill process
```go
cmd := client.Command("pwd")
// ......
err := cmd.Kill()
```
