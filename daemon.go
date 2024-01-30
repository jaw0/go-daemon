// Copyright (c) 2018
// Author: Jeff Weisberg <jaw @ tcp4me.com>
// Created: 2018-Jul-20 22:43 (EDT)
// Function: run as a daemon

// run as a daemon
package daemon

import (
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"
)

const (
	ExitFinished = 0
	ExitRestart  = 1
)

const ENVVAR = "_dmode"

type opts struct {
	keepStderr   bool
	justOne      bool
	testDelay    bool
	restartDelay time.Duration
	pidFile      string
}
type optFunc func(*opts)

// daemon.Ize(WithOpts...) - run program as a daemon
func Ize(optfn ...optFunc) {

	opt := &opts{
		restartDelay: 5 * time.Second,
	}
	for _, fn := range optfn {
		fn(opt)
	}

	mode := os.Getenv(ENVVAR)
	prog, err := os.Executable()

	if err != nil {
		fmt.Printf("cannot daemonize: %v", err)
		os.Exit(2)
	}

	if mode == "" {
		// initial execution
		// switch to the background
		if opt.justOne {
			// only run the main program as a daemon
			os.Setenv(ENVVAR, "2")
		} else {
			// run the main program + watcher as daemons
			os.Setenv(ENVVAR, "1")
		}
		dn, _ := os.OpenFile(os.DevNull, os.O_RDWR, 0666)
		pa := &os.ProcAttr{Files: []*os.File{dn, dn, os.Stderr}}
		if !opt.keepStderr {
			pa.Files[2] = dn
		}
		os.StartProcess(prog, os.Args, pa)
		if opt.testDelay {
			// 'go test' will delete the executable file, take a pause
			time.Sleep(1 * time.Second)
		}
		os.Exit(0)
	}

	syscall.Setsid()

	if mode == "2" {
		// run and be the main program
		return
	}

	var sigchan = make(chan os.Signal, 5)
	signal.Notify(sigchan, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT, syscall.SIGHUP)

	if opt.pidFile != "" {
		opt.savePidFile()
	}

	// watch + restart
	for {
		os.Setenv(ENVVAR, "2")
		dn, _ := os.OpenFile(os.DevNull, os.O_RDWR, 0666)
		pa := &os.ProcAttr{Files: []*os.File{dn, dn, os.Stderr}}
		if !opt.keepStderr {
			pa.Files[2] = dn
		}
		p, err := os.StartProcess(prog, os.Args, pa)
		if err != nil {
			fmt.Printf("cannot start %s: %v", prog, err)
			os.Exit(2)
		}

		stop := make(chan struct{})
		var wg sync.WaitGroup
		wg.Add(1)

		go func() {
			defer wg.Done()
			select {
			case <-stop:
				return
			case n := <-sigchan:
				// pass the signal on through to the running program
				p.Signal(n)
			}
		}()

		st, _ := p.Wait()
		if !st.Exited() {
			continue
		}
		if st.Success() {
			// done
			if opt.pidFile != "" {
				opt.removePidFile()
			}
			os.Exit(0)
		}

		close(stop)
		wg.Wait()
		time.Sleep(opt.restartDelay)
	}
}

func (o *opts) savePidFile() error {

	f, err := os.Create(o.pidFile)
	if err != nil {
		return err
	}

	fmt.Fprintf(f, "%d\n", os.Getpid())

	prog, err := os.Executable()
	if err == nil {
		f.WriteString(fmt.Sprintf("# %s", prog))
		for _, arg := range os.Args[1:] {
			f.WriteString(" ")
			f.WriteString(arg)
		}
		f.WriteString("\n")
	}

	f.Close()
	return nil
}

func (o *opts) removePidFile() {
	os.Remove(o.pidFile)
}

func SigExiter() {
	var sigchan = make(chan os.Signal, 5)
	signal.Notify(sigchan, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT, syscall.SIGHUP)

	select {
	case n := <-sigchan:
		switch n {
		case syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT:
			os.Exit(0)
		case syscall.SIGHUP:
			os.Exit(1)
		default:
			os.Exit(2)
		}
	}
}

// WithPidFile(filename) - specify a pidfile
func WithPidFile(file string) func(*opts) {
	return func(opt *opts) {
		opt.pidFile = file
	}
}

// WithNoRestart() - don't run a 2nd daemon to watch + restart
func WithNoRestart() func(*opts) {
	return func(opt *opts) {
		opt.justOne = true
	}
}

// WithRestartDelay(time.Duration) - delay restart when running WithStayAlive
func WithRestartDelay(d time.Duration) func(*opts) {
	return func(opt *opts) {
		opt.restartDelay = d
	}
}

// WithStderr() - keep stderr open for output
func WithStderr() func(*opts) {
	return func(opt *opts) {
		opt.keepStderr = true
	}
}

func WithTestDelay() func(*opts) {
	return func(opt *opts) {
		opt.testDelay = true
	}
}
