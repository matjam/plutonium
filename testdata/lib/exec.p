package main

// pluto/os/exec: running commands with their streams connected to
// /dev/null, to files, and through pipes to writers, readers and vectors;
// exit statuses, signals, failures to start, the working directory, the
// environment and LookPath.

import (
	"pluto/bytes"
	"pluto/errors"
	"pluto/os"
	"pluto/os/exec"
	"pluto/strings"
	"unsafe"
)

func str(b []byte) string { return unsafe.String(unsafe.SliceData(b), len(b)) }

func sh(script string) exec.Cmd { return exec.Command("sh", "-c", script) }

func main() {
	// Output captures standard output; standard error goes to /dev/null.
	c := sh("echo hello; echo hidden >&2")
	out, err := c.Output()
	print("output: ", str(out.Slice()))
	println("err", err == nil, c.ProcessState.ExitCode(), c.ProcessState.Success())
	out.Free()
	c.Drop()

	// CombinedOutput shares one pipe.
	c = sh("echo one; echo two >&2; echo three")
	out, err = c.CombinedOutput()
	print("combined: ", str(out.Slice()))
	println("err", err == nil)
	out.Free()
	c.Drop()

	// Separate writers: a strings.Builder for each stream.
	var so, se strings.Builder
	c = sh("printf 'to out'; printf 'to err' >&2")
	c.Stdout = &so
	c.Stderr = &se
	err = c.Run()
	println("builders", so.String(), "|", se.String(), err == nil)
	so.Free()
	se.Free()
	c.Drop()

	// Standard input from a reader, piped through cat, larger than a pipe
	// holds so that writing and reading must interleave.
	var src [100000]byte
	for i := range src {
		src[i] = byte('a' + i%26)
	}
	in := bytes.NewBuffer(src[:])
	in.Write(src[:])
	c = exec.Command("cat")
	c.Stdin = &in
	out, err = c.Output()
	same := out.Len() == len(src)
	for i := 0; same && i < len(src); i++ {
		same = out.At(i) == src[i]
	}
	println("cat", out.Len(), same, err == nil)
	out.Free()
	c.Drop()

	// A child writing straight to our standard output file.
	c = sh("echo straight to stdout")
	c.Stdout = os.Stdout
	println("file stdout", c.Run() == nil)
	c.Drop()

	// Exit statuses and signals.
	c = sh("exit 3")
	err = c.Run()
	var ee exec.ExitError
	println("exit 3:", errors.As(err, &ee), ee.ExitCode(), ee.Exited(), err.Error(), c.ProcessState.ExitCode())
	c.Drop()
	c = sh("kill -9 $$")
	err = c.Run()
	println("killed:", errors.As(err, &ee), ee.ExitCode(), ee.Exited(), err.Error())
	c.Drop()
	c = sh("exit 255")
	println("exit 255:", c.Run().Error())
	c.Drop()

	// The working directory and the environment.
	c = exec.Command("pwd")
	c.Dir = "/"
	out, err = c.Output()
	print("pwd: ", str(out.Slice()))
	out.Free()
	c.Drop()
	c = sh("echo $GREETING-$HOME")
	c.Env = []string{"GREETING=hi", "HOME=nowhere"}
	out, err = c.Output()
	print("env: ", str(out.Slice()))
	out.Free()
	c.Drop()

	// Failures to start.
	c = exec.Command("surely-not-a-command-anywhere")
	err = c.Run()
	println("not found:", errors.Is(err, exec.ErrNotFound), err.Error())
	errors.Free(err)
	c.Drop()
	c = exec.Command("/")
	err = c.Run()
	var pe *os.PathError
	println("directory:", errors.As(err, &pe), pe.Op, errors.Is(err, os.ErrPermission) || err != nil)
	errors.Free(err)
	c.Drop()
	c = exec.Command("/dev/null/x")
	err = c.Start()
	println("bad path:", err != nil)
	errors.Free(err)
	c.Drop()

	// LookPath.
	var buf [256]byte
	p, err := exec.LookPath("sh", buf[:])
	println("lookpath sh", strings.HasSuffix(p, "/sh"), err == nil)
	p, err = exec.LookPath("./relative", buf[:])
	println("lookpath relative", p == "", err != nil)
	errors.Free(err)

	// Misuse.
	c = sh("true")
	println("wait before start", c.Wait() == exec.ErrNotStarted)
	println("run", c.Run() == nil, "start again", c.Start() == exec.ErrAlreadyStarted)
	c.Drop()
	c = sh("true")
	c.Stdout = os.Stdout
	_, err = c.Output()
	println("output with stdout", err == exec.ErrStdoutSet)
	c.Drop()
}
