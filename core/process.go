package core

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

type childProcess struct {
	cmd    *exec.Cmd
	done   chan struct{}
	addr   string
	output *processOutput
}

type processOutput struct {
	mu   sync.Mutex
	tail string
}

func (o *processOutput) Write(b []byte) (int, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.tail += string(b)
	if len(o.tail) > 2048 {
		o.tail = o.tail[len(o.tail)-2048:]
	}
	return len(b), nil
}
func (o *processOutput) String() string {
	if o == nil {
		return ""
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	return strings.TrimSpace(o.tail)
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
	output := &processOutput{}
	cmd.Stderr = output
	if err = cmd.Start(); err != nil {
		return nil, err
	}
	child := &childProcess{cmd: cmd, done: make(chan struct{}), addr: net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), output: output}
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
			return nil, fmt.Errorf("ciadpi exited during startup: %s", output.String())
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
