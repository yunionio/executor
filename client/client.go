package client

import (
	"bytes"
	"context"
	"io"
	"net"
	"os"
	"syscall"
	"time"

	"github.com/pkg/errors"
	"google.golang.org/grpc"
	"yunion.io/x/executor/apis"
	"yunion.io/x/executor/utils"
)

var exec *Executor

type Executor struct {
	socketPath string
}

func Init(socketPath string) {
	exec = &Executor{socketPath}
}

func Command(path string, args ...string) *Cmd {
	if exec == nil {
		panic("executor not init ???")
	}
	return &Cmd{
		Executor: exec,
		Path:     path,
		Args:     args,
	}
}

type Cmd struct {
	*Executor

	Path string
	Args []string
	Env  []string
	Dir  string

	conn   *grpc.ClientConn
	client apis.ExecutorClient

	fetchError chan error
	// Proc       *Process

	sn *apis.Sn

	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer

	closeAfterWait []io.Closer
	goroutine      []func() error
	errch          chan error
}

func grcpDialWithUnixSocket(ctx context.Context, socketPath string) (*grpc.ClientConn, error) {
	return grpc.DialContext(
		ctx, socketPath,
		grpc.WithInsecure(), grpc.WithBlock(), grpc.WithTimeout(time.Second*3),
		grpc.WithDialer(func(addr string, timeout time.Duration) (net.Conn, error) {
			return net.DialTimeout("unix", addr, timeout)
		}),
	)
}

func (c *Cmd) Connect(ctx context.Context, opts ...grpc.CallOption,
) error {
	var err error
	c.conn, err = grcpDialWithUnixSocket(ctx, c.socketPath)
	if err != nil {
		return errors.Wrap(err, "grpc dial error")
	}
	c.client = apis.NewExecutorClient(c.conn)
	return nil
}

func (c *Cmd) Run() error {
	if err := c.Start(); err != nil {
		return err
	}
	return c.Wait()
}

func (c *Cmd) CombinedOutput() ([]byte, error) {
	if c.Stdout != nil {
		return nil, errors.New("exec: Stdout already set")
	}
	if c.Stderr != nil {
		return nil, errors.New("exec: Stderr already set")
	}
	var b bytes.Buffer
	c.Stdout = &b
	c.Stderr = &b
	err := c.Run()
	return b.Bytes(), err
}

func (c *Cmd) Output() ([]byte, error) {
	if c.Stdout != nil {
		return nil, errors.New("exec: Stdout already set")
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	c.Stdout = &stdout
	c.Stderr = &stderr

	// function run return err its mean grpc stream transport error
	// cmd execute error indicate by exit code
	if err := c.Run(); err != nil {
		if e, ok := err.(*ExitError); ok {
			e.Stderr = stderr.Bytes()
		}
		return nil, err
	}
	return stdout.Bytes(), nil
}

func (c *Cmd) Start() error {
	if c.conn != nil {
		return errors.New("cmd executing")
	}
	if err := c.Connect(context.Background()); err != nil {
		return err
	}

	sn, err := c.client.ExecCommand(context.Background(), &apis.Command{
		Path: []byte(c.Path),
		Args: utils.StrArrayToBytesArray(c.Args),
		Env:  utils.StrArrayToBytesArray(c.Env),
		Dir:  []byte(c.Dir),
	})
	if err != nil {
		return errors.Wrap(err, "grcp exec command")
	}
	c.sn = sn

	res, err := c.client.Start(context.Background(), c.sn)
	if err != nil {
		return errors.Wrap(err, "grpc start cmd")
	}
	if !res.Success {
		return errors.New(string(res.Error))
	} else {
		if err = c.ioStream(); err != nil {
			return err
		}

		return nil
	}
}

// TODO: 关闭conn连接
func (c *Cmd) Wait() error {
	if c.conn == nil {
		return errors.New("cmd not executing")
	}
	res, err := c.client.Wait(context.Background(), c.sn)
	if err != nil {
		return errors.Wrap(err, "grpc wait proc")
	}
	if len(res.ErrContent) > 0 {
		return errors.New(string(res.ErrContent))
	}

	var copyError error
	for range c.goroutine {
		if err := <-c.errch; err != nil && copyError == nil {
			copyError = err
		}
	}

	c.closeDescriptors(c.closeAfterWait)

	if res.ExitStatus == 0 {
		if copyError != nil {
			return copyError
		}
		return nil
	} else {
		return &ExitError{ExitStatus: syscall.WaitStatus(res.ExitStatus)}
	}
}

func (c *Cmd) closeDescriptors(closers []io.Closer) {
	for _, fd := range closers {
		fd.Close()
	}
}

func (c *Cmd) ioStream() error {
	var procIO = [3]*os.File{}

	type F func(*Cmd) (*os.File, error)
	for i, setupFd := range [3]F{(*Cmd).stdin, (*Cmd).stdout, (*Cmd).stderr} {
		fd, err := setupFd(c)
		if err != nil {
			c.closeDescriptors(c.closeAfterWait)
			return err
		}
		procIO[i] = fd
	}
	go c.sendStdin(procIO[0])
	go c.fetchStdout(procIO[1])
	go c.fetchStderr(procIO[2])

	c.errch = make(chan error, len(c.goroutine))
	for _, fn := range c.goroutine {
		go func(fn func() error) {
			c.errch <- fn()
		}(fn)
	}
	return nil
}

func (c *Cmd) StdinPipe() (io.WriteCloser, error) {
	if c.Stdin != nil {
		return nil, errors.New("exec: Stdin already set")
	}
	if c.conn != nil {
		return nil, errors.New("exec: StdinPipe after process started")
	}
	// do not use io.Pipe, block forever
	// https://stackoverflow.com/questions/47486128
	pr, pw, err := os.Pipe()
	if err != nil {
		return nil, errors.Wrap(err, "open stdinpipe")
	}
	c.Stdin = pr
	c.closeAfterWait = append(c.closeAfterWait, pr)
	wc := &utils.CloseOnce{File: pw}
	c.closeAfterWait = append(c.closeAfterWait, wc)
	return wc, nil
}

func (c *Cmd) stdin() (*os.File, error) {
	if c.Stdin == nil {
		return nil, nil
	}

	if f, ok := c.Stdin.(*os.File); ok {
		return f, nil
	}

	pr, pw, err := os.Pipe()
	if err != nil {
		return nil, err
	}

	// c.closeAfterStart = append(c.closeAfterStart, pr)
	c.closeAfterWait = append(c.closeAfterWait, pr)
	c.goroutine = append(c.goroutine, func() error {
		_, err := io.Copy(pw, c.Stdin)
		if err1 := pw.Close(); err == nil {
			err = err1
		}
		return err
	})
	return pr, nil
}

func (c *Cmd) stdout() (f *os.File, err error) {
	return c.writerDescriptor(c.Stdout)
}

func (c *Cmd) stderr() (f *os.File, err error) {
	return c.writerDescriptor(c.Stderr)
}

func (c *Cmd) writerDescriptor(w io.Writer) (*os.File, error) {
	if w == nil {
		f, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
		if err != nil {
			return nil, err
		}
		c.closeAfterWait = append(c.closeAfterWait, f)
		return f, nil
	}

	if f, ok := w.(*os.File); ok {
		return f, nil
	}

	pr, pw, err := os.Pipe()
	if err != nil {
		return nil, err
	}

	c.closeAfterWait = append(c.closeAfterWait, pw)
	c.goroutine = append(c.goroutine, func() error {
		_, err := io.Copy(w, pr)
		pr.Close() // in case io.Copy stopped due to write error
		return err
	})
	return pw, nil
}

func (c *Cmd) StdoutPipe() (io.ReadCloser, error) {
	if c.Stdout != nil {
		return nil, errors.New("exec: Stdout already set")
	}
	if c.conn != nil {
		return nil, errors.New("exec: StdoutPipe after process started")
	}
	pr, pw, err := os.Pipe()
	if err != nil {
		return nil, errors.Wrap(err, "open stdoutpipe")
	}
	c.Stdout = pw
	c.closeAfterWait = append(c.closeAfterWait, pw)
	wc := &utils.CloseOnce{File: pr}
	c.closeAfterWait = append(c.closeAfterWait, wc)
	return wc, nil
}

func (c *Cmd) StderrPipe() (io.ReadCloser, error) {
	if c.Stderr != nil {
		return nil, errors.New("exec: Stderr already set")
	}
	if c.conn != nil {
		return nil, errors.New("exec: StderrPipe after process started")
	}
	pr, pw, err := os.Pipe()
	if err != nil {
		return nil, errors.Wrap(err, "open stderrpipe")
	}
	c.Stderr = pw
	c.closeAfterWait = append(c.closeAfterWait, pw)
	wc := &utils.CloseOnce{File: pr}
	c.closeAfterWait = append(c.closeAfterWait, wc)
	return wc, nil
}

func (c *Cmd) sendStdin(r io.Reader) error {
	stream, err := c.client.SendInput(context.Background())
	if err != nil {
		return errors.Wrap(err, "grpc send input")
	}

	var data = make([]byte, 4096)
	for {
		n, err := r.Read(data)
		if err == io.EOF {
			e, err := stream.CloseAndRecv()
			if err != nil {
				return errors.Wrap(err, "grpc send stdin on close and recv")
			}
			if len(e.Error) > 0 {
				return errors.New(string(e.Error))
			}
			return nil
		} else if err != nil {
			return errors.Wrap(err, "read from stdin")
		}
		err = stream.Send(&apis.Input{
			Sn:    c.sn.Sn,
			Input: data[:n],
		})
		if err != nil {
			return err
		}
	}
}

func (c *Cmd) fetchStdout(w io.WriteCloser) error {
	stream, err := c.client.FetchStdout(context.Background(), c.sn)
	if err != nil {
		return errors.Wrap(err, "grpc fetch stdout")
	}
	defer w.Close()
	// defer c.stdout.close() 这个样子比较好
	for {
		data, err := stream.Recv()
		if err == io.EOF {
			return nil
		} else if err != nil {
			return errors.Wrap(err, "grpc stdout recv")
		}
		if data.Closed {
			return nil
		} else if len(data.RuntimeError) > 0 {
			return errors.New(string(data.RuntimeError))
		} else {
			err := utils.WriteTo(data.Stdout, w)
			if err != nil {
				return errors.Wrap(err, "write to stdout")
			}
		}
	}
}

func (c *Cmd) fetchStderr(w io.WriteCloser) error {
	stream, err := c.client.FetchStderr(context.Background(), c.sn)
	if err != nil {
		return errors.Wrap(err, "grpc fetch stdout")
	}
	defer w.Close()
	// defer c.stderr.close() 这个样子比较好
	for {
		data, err := stream.Recv()
		if err == io.EOF {
			return nil
		} else if err != nil {
			return errors.Wrap(err, "grpc stderr recv")
		}
		if data.Closed {
			return nil
		} else if len(data.RuntimeError) > 0 {
			return errors.New(string(data.RuntimeError))
		} else {
			err := utils.WriteTo(data.Stderr, w)
			if err != nil {
				return errors.Wrap(err, "write to stderr")
			}
		}
	}
}

// Convert integer to decimal string
func itoa(val int) string {
	if val < 0 {
		return "-" + uitoa(uint(-val))
	}
	return uitoa(uint(val))
}

// Convert unsigned integer to decimal string
func uitoa(val uint) string {
	if val == 0 { // avoid string allocation
		return "0"
	}
	var buf [20]byte // big enough for 64bit value base 10
	i := len(buf) - 1
	for val >= 10 {
		q := val / 10
		buf[i] = byte('0' + val - q*10)
		i--
		val = q
	}
	// val < 10
	buf[i] = byte('0' + val)
	return string(buf[i:])
}

// Convert exit status to error string
// Source code in exec posix
func exitStatusToString(status syscall.WaitStatus) string {
	res := ""
	switch {
	case status.Exited():
		res = "exit status " + itoa(status.ExitStatus())
	case status.Signaled():
		res = "signal: " + status.Signal().String()
	case status.Stopped():
		res = "stop signal: " + status.StopSignal().String()
		if status.StopSignal() == syscall.SIGTRAP && status.TrapCause() != 0 {
			res += " (trap " + itoa(status.TrapCause()) + ")"
		}
	case status.Continued():
		res = "continued"
	}
	if status.CoreDump() {
		res += " (core dumped)"
	}
	return res
}

type ExitError struct {
	ExitStatus syscall.WaitStatus
	Stderr     []byte
}

func (e *ExitError) Sys() interface{} {
	return e.ExitStatus
}

func (e *ExitError) Error() string {
	return exitStatusToString(e.ExitStatus)
}

type BufCloser struct {
	*bytes.Buffer
}

func NewBufCloser(buf *bytes.Buffer) *BufCloser {
	return &BufCloser{buf}
}

func (bc *BufCloser) Close() error {
	return nil
}
