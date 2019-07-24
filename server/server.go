package server

import (
	"context"
	"io"
	"os/exec"
	"sync"
	"sync/atomic"
	"syscall"

	"github.com/pkg/errors"

	"yunion.io/x/executor/apis"
	"yunion.io/x/executor/utils"
)

var sn int32

func GetSN() uint32 {
	return uint32(atomic.AddInt32(&sn, 1))
}

var socketPath string

func Init(socketPath string) {
	socketPath = socketPath
	cmds = &sync.Map{}
}

var cmds *sync.Map

type Executor struct{}

func (e *Executor) ExecCommand(ctx context.Context, req *apis.Command) (*apis.Sn, error) {
	cm := NewCommander(req)
	sn := GetSN()
	cmds.Store(sn, cm)
	return &apis.Sn{Sn: sn}, nil
}

func (e *Executor) Start(ctx context.Context, req *apis.Sn) (*apis.StartResponse, error) {
	icm, ok := cmds.Load(req.Sn)
	if !ok {
		return nil, errors.New("unknown sn")
	}
	var (
		m   = icm.(*Commander)
		err error
	)
	m.stdin, err = m.c.StdinPipe()
	if err != nil {
		return &apis.StartResponse{
			Success: false,
			Error:   []byte(err.Error()),
		}, nil
	}

	m.stdout, err = m.c.StdoutPipe()
	if err != nil {
		return &apis.StartResponse{
			Success: false,
			Error:   []byte(err.Error()),
		}, nil
	}

	m.stderr, err = m.c.StderrPipe()
	if err != nil {
		return &apis.StartResponse{
			Success: false,
			Error:   []byte(err.Error()),
		}, nil
	}
	if err := m.c.Start(); err != nil {
		return &apis.StartResponse{
			Success: false,
			Error:   []byte(err.Error()),
		}, nil
	}
	return &apis.StartResponse{
		Success: true,
		Error:   []byte(err.Error()),
	}, nil
}

func (e *Executor) Wait(ctx context.Context, in *apis.Sn) (*apis.WaitResponse, error) {
	icm, ok := cmds.Load(in.Sn)
	if !ok {
		return nil, errors.New("unknown sn")
	}
	var (
		m   = icm.(*Commander)
		err error
	)
	err = m.c.Wait()
	var (
		exitStatus uint32
		errContent string
	)
	if err != nil {
		if exiterr, ok := err.(*exec.ExitError); ok {
			// The program has exited with an exit code != 0
			// This works on both Unix and Windows. Although package
			// syscall is generally platform dependent, WaitStatus is
			// defined for both Unix and Windows and in both cases has
			// an ExitStatus() method with the same signature.
			exitStatus = uint32(exiterr.Sys().(syscall.WaitStatus))
		} else {
			// command not found or io problem
			errContent = err.Error()
		}
	} else {
		exitStatus = 0
	}
	cmds.Delete(in.Sn) // 其他地方也能也需要delete
	return &apis.WaitResponse{
		ExitStatus: exitStatus,
		ErrContent: []byte(errContent),
	}, nil
}

func (e *Executor) SendInput(s apis.Executor_SendInputServer) error {
	var m *Commander
	for {
		input, err := s.Recv()
		if err == io.EOF {
			return s.SendAndClose(&apis.Error{})
		} else if err != nil {
			return s.SendAndClose(&apis.Error{
				Error: []byte(err.Error()),
			})
		}
		if m == nil {
			icm, ok := cmds.Load(input.Sn)
			if !ok {
				return errors.New("unknown sn")
			}
			m = icm.(*Commander)
		}
		_, err = m.stdin.Write(input.Input)
		if err != nil {
			return s.SendAndClose(&apis.Error{
				Error: []byte(err.Error()),
			})
		}
	}
}

func (e *Executor) FetchStdout(sn *apis.Sn, s apis.Executor_FetchStdoutServer) error {
	icm, ok := cmds.Load(sn.Sn)
	if !ok {
		return errors.New("unknown sn")
	}
	var (
		m    = icm.(*Commander)
		data = make([]byte, 4096)
		err  error
		n    int
	)
	for {
		n, err = m.stdout.Read(data)
		if err == io.EOF {
			return s.Send(&apis.Stdout{Closed: true})
		} else {
			return s.Send(&apis.Stdout{RuntimeError: []byte(err.Error())})
		}
		err = s.Send(&apis.Stdout{Stdout: data[:n]})
		if err != nil {
			return err
		}
	}
}

func (e *Executor) FetchStderr(sn *apis.Sn, s apis.Executor_FetchStderrServer) error {
	icm, ok := cmds.Load(sn.Sn)
	if !ok {
		return errors.New("unknown sn")
	}
	var (
		m    = icm.(*Commander)
		data = make([]byte, 4096)
		err  error
		n    int
	)
	for {
		n, err = m.stderr.Read(data)
		if err == io.EOF {
			return s.Send(&apis.Stderr{Closed: true})
		} else {
			return s.Send(&apis.Stderr{RuntimeError: []byte(err.Error())})
		}
		err = s.Send(&apis.Stderr{Stderr: data[:n]})
		if err != nil {
			return err
		}
	}
}

type Commander struct {
	// stream apis.Executor_ExecCommandServer

	c      *exec.Cmd
	stdin  io.WriteCloser
	stdout io.ReadCloser
	stderr io.ReadCloser
}

func NewCommander(in *apis.Command) *Commander {
	cmd := exec.Command(string(in.Path), utils.BytesArrayToStrArray(in.Args)...)
	if len(in.Env) > 0 {
		cmd.Env = utils.BytesArrayToStrArray(in.Env)
	}
	if len(in.Dir) > 0 {
		cmd.Dir = string(in.Dir)
	}
	return &Commander{
		c: cmd,
	}
}

// func (m *Commander) Start() error {
// 	for {
// 		cmd, err := m.stream.Recv()
// 		if err == io.EOF {
// 			return nil
// 		}
// 		if err != nil {
// 			return err
// 		}
// 		switch {
// 		case len(cmd.Path) > 0:
// 			if m.c != nil {
// 				return errors.New("cmd executed")
// 			}
// 			m.c = exec.Command(string(cmd.Path), utils.BytesArrayToStrArray(cmd.Args)...)
// 			m.c.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
// 			if len(cmd.Env) > 0 {
// 				m.c.Env = utils.BytesArrayToStrArray(cmd.Env)
// 			}
// 			if len(cmd.Dir) > 0 {
// 				m.c.Dir = string(cmd.Dir)
// 			}
// 			m.stdin, err = m.c.StdinPipe()
// 			if err != nil {
// 				return m.handleErr(err)
// 			}

// 			m.stdout, err = m.c.StdoutPipe()
// 			if err != nil {
// 				return m.handleErr(err)
// 			}

// 			m.stderr, err = m.c.StderrPipe()
// 			if err != nil {
// 				return m.handleErr(err)
// 			}
// 			if err := m.c.Start(); err != nil {
// 				return m.handleErr(err)
// 			}
// 			m.readOutput()
// 		case len(cmd.Input) > 0:
// 			err := m.sendInput(cmd.Input)
// 			if err != nil {
// 				return errors.Wrap(err, "send input")
// 			}
// 		case cmd.KillProcess:
// 			err := m.killPorc()
// 			if err != nil {
// 				return m.handleErr(err)
// 			}
// 			return nil
// 		case cmd.Wait:
// 			err := m.waitPorc()
// 			if err != nil {
// 				return m.handleErr(err)
// 			}
// 			return nil
// 		}
// 	}
// }

// func (m *Commander) sendInput(data []byte) error {
// 	var n = 0
// 	var length = len(data)
// 	for n < length {
// 		r, e := m.stdin.Write(data[n:])
// 		if e != nil {
// 			return e
// 		}
// 		n += r
// 	}
// 	return nil
// }

// func (m *Commander) readOutput() {
// 	m.streamData(m.stdout, STDOUT)
// 	m.streamData(m.stderr, STDERR)
// }

// func (m *Commander) streamData(r io.Reader, kind int) {
// 	// m.wg.Add(1)
// 	go func() {
// 		// defer m.wg.Done()
// 		var data = make([]byte, 4069)
// 		for {
// 			n, err := r.Read(data)
// 			if err == io.EOF {
// 				if err = m.streamEof(kind); err != nil {
// 					log.Errorf("%d Stream EOF Failed %s", kind, err)
// 				}
// 				break
// 			} else if err != nil {
// 				// os/exec.Command.Wait will close pipe
// 				if pe, ok := err.(*os.PathError); ok && pe.Err == os.ErrClosed {
// 					err := m.streamEof(kind)
// 					if err != nil {
// 						log.Errorf("%d Stream EOF Failed %s", kind, err)
// 					}
// 					break
// 				} else {
// 					if err := m.streamError(errors.Wrap(err, "read stream data")); err != nil {
// 						log.Errorf("%d Stream Error Failed %s", kind, err)
// 					}
// 					break
// 				}
// 			}
// 			if n > 0 {
// 				err = m.stream.Send(&apis.Response{Stdout: data[:n]})
// 				if err != nil {
// 					if err := m.streamError(errors.Wrap(err, "stream send")); err != nil {
// 						log.Errorf("%d Stream Error Failed %s", kind, err)
// 					}
// 					break
// 				}
// 			}
// 		}
// 	}()
// }

// const (
// 	STDIN  = 0
// 	STDOUT = 1
// 	STDERR = 2
// )

// func (m *Commander) streamEof(kind int) error {
// 	switch kind {
// 	case STDOUT:
// 		return m.stream.Send(&apis.Response{StdoutClosed: true})
// 	case STDERR:
// 		return m.stream.Send(&apis.Response{StderrClosed: true})
// 	default:
// 		return errors.New("Unkonwn type of EOF")
// 	}
// }

// func (m *Commander) killPorc() error {
// 	if m.c != nil && m.c.Process != nil {
// 		return m.c.Process.Kill()
// 	}
// 	return errors.New("No such process to kill")
// }

// func (m *Commander) waitPorc() error {
// 	if m.c != nil {
// 		return m.c.Wait()
// 	}
// 	return errors.New("No such process to wait")
// }

// func (m *Commander) handleErr(err error) error {
// 	var (
// 		exitStatus uint32
// 		errContent string
// 	)
// 	if err != nil {
// 		if exiterr, ok := err.(*exec.ExitError); ok {
// 			// The program has exited with an exit code != 0
// 			// This works on both Unix and Windows. Although package
// 			// syscall is generally platform dependent, WaitStatus is
// 			// defined for both Unix and Windows and in both cases has
// 			// an ExitStatus() method with the same signature.
// 			exitStatus = uint32(exiterr.Sys().(syscall.WaitStatus))
// 		} else {
// 			// command not found or io problem
// 			errContent = err.Error()
// 		}
// 	} else {
// 		exitStatus = 0
// 	}
// 	return m.stream.Send(&apis.Response{
// 		ExitStatus: exitStatus,
// 		IsExit:     true,
// 		ErrContent: []byte(errContent),
// 	})
// }

// func (m *Commander) streamError(err error) error {
// 	return m.stream.Send(&apis.Response{RuntimeError: []byte(err.Error())})
// }
