package core

import (
	"context"
	"errors"
	"net"
	"os/exec"
	"strconv"
	"time"
)

type childProcess struct {
	cmd  *exec.Cmd
	done chan struct{}
	addr string
}

func startChild(ctx context.Context, executable, fake string, s Strategy) (*childProcess, error) {
	l, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()
	args := []string{"-i", "127.0.0.1", "-p", strconv.Itoa(port), "-N", "-U", "-c", "192"}
	for _, a := range s.Args {
		if a == "@google" {
			a = fake
		}
		args = append(args, a)
	}
	cmd := exec.CommandContext(ctx, executable, args...)
	if err = cmd.Start(); err != nil {
		return nil, err
	}
	child := &childProcess{cmd: cmd, done: make(chan struct{}), addr: net.JoinHostPort("127.0.0.1", strconv.Itoa(port))}
	go func() { cmd.Wait(); close(child.done) }()
	timeout := time.NewTimer(3 * time.Second)
	defer timeout.Stop()
	tick := time.NewTicker(30 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			child.stop()
			return nil, ctx.Err()
		case <-child.done:
			return nil, errors.New("ciadpi exited during startup")
		case <-timeout.C:
			child.stop()
			return nil, errors.New("ciadpi startup timeout")
		case <-tick.C:
			c, e := net.DialTimeout("tcp", child.addr, 50*time.Millisecond)
			if e == nil {
				c.Close()
				return child, nil
			}
		}
	}
}
func (p *childProcess) stop() {
	if p == nil {
		return
	}
	_ = p.cmd.Process.Kill()
	<-p.done
}
