package main

import (
	"bufio"
	"bytes"
	"io"
	"log"
	"os"
	"os/exec"
	"sync/atomic"
	"syscall"
	"time"

	"code.linksmart.eu/dt/deployment-tool/model"
	"github.com/mholt/archiver"
)

type executor struct {
	workDir string
	cmd     *exec.Cmd
}

func newExecutor(workDir string) *executor {
	return &executor{workDir: workDir}
}

func (e *executor) storeArtifacts(b []byte) {
	log.Printf("Deploying %d bytes of artifacts.", len(b))
	err := archiver.TarGz.Read(bytes.NewBuffer(b), e.workDir)
	if err != nil {
		log.Fatal(err)
	}
}

func (e *executor) responseBatchCollector(task *model.Task, out chan model.BatchResponse) {

	batch := model.BatchResponse{
		ResponseType: model.ResponseLog,
		TaskID:       task.ID,
	}

	// logging attributes
	interval, err := time.ParseDuration(task.Log.Interval)
	if err != nil {
		log.Println(err)
		batch.ResponseType = model.ResponseClientError
		out <- batch
		return
	}
	log.Println("Will send logs every", interval)

	resCh := make(chan model.Response)
	go e.responseCollector(task.Commands, resCh)
	var containsErrors bool
	ticker := time.NewTicker(interval)
LOOP:
	for {
		select {
		case res, open := <-resCh:
			if !open {
				break LOOP
			}
			log.Printf("[res] %+v", res)
			containsErrors = len(res.Stderr) > 0
			//log.Printf("%s -- %d -- %s -- %s -- %f", res.Command, res.LineNum, res.Stdout, res.Stderr, res.TimeElapsed)
			batch.Responses = append(batch.Responses, res)
			batch.TimeElapsed = res.TimeElapsed
		case <-ticker.C:
			if len(batch.Responses) == 0 {
				break
			}
			out <- batch
			log.Printf("Batch: %+v", batch)

			// flush responses
			batch.Responses = []model.Response{}
		}
	}
	if containsErrors {
		batch.ResponseType = model.ResponseError
	} else {
		batch.ResponseType = model.ResponseSuccess
	}

	out <- batch
	log.Printf("Final Batch: %+v", batch)
}

func (e *executor) responseCollector(commands []string, out chan model.Response) {
	start := time.Now()

	stdout, stderr := make(chan logLine), make(chan logLine)
	callback := make(chan error)

	go e.executeMultiple(commands, stdout, stderr, callback)

	for open := true; open; {
		select {
		case x := <-stdout:
			out <- model.Response{Command: x.command, Stdout: x.line, LineNum: x.lineNum, TimeElapsed: time.Since(start).Seconds()}
		case x := <-stderr:
			out <- model.Response{Command: x.command, Stderr: x.line, LineNum: x.lineNum, TimeElapsed: time.Since(start).Seconds()}
		case _, open = <-callback:
			// do nothing
		}
	}

	//log.Println("closing responseCollector")
	close(out)
}

func (e *executor) executeMultiple(commands []string, stdout, stderr chan logLine, callback chan error) {
	for _, command := range commands {
		e.execute(command, stdout, stderr)
	}
	close(callback)
}

// one line of log for a command
type logLine struct {
	command string
	line    string
	lineNum uint32
}

func (e *executor) execute(command string, stdout, stderr chan logLine) {
	bashCommand := []string{"/bin/sh", "-c", command}
	e.cmd = exec.Command(bashCommand[0], bashCommand[1:]...)

	e.cmd.Dir = e.workDir
	e.cmd.SysProcAttr = &syscall.SysProcAttr{}
	e.cmd.SysProcAttr.Setsid = true

	var line uint32

	outStream, err := e.cmd.StdoutPipe()
	if err != nil {
		log.Fatal(err)
	}

	errStream, err := e.cmd.StderrPipe()
	if err != nil {
		log.Fatal(err)
	}

	// stdout reader
	go func(stream io.ReadCloser) {
		scanner := bufio.NewScanner(stream)

		for scanner.Scan() {
			atomic.AddUint32(&line, 1)
			//log.Println(scanner.Text())
			stdout <- logLine{command, scanner.Text(), line}
		}
		if err = scanner.Err(); err != nil {
			stderr <- logLine{command, err.Error(), line}
			log.Println("Error:", err)
		}
		stream.Close()
	}(outStream)

	// stderr reader
	go func(stream io.ReadCloser) {
		scanner := bufio.NewScanner(stream)

		for scanner.Scan() {
			atomic.AddUint32(&line, 1)
			//log.Println("stderr:", scanner.Text())
			stderr <- logLine{command, scanner.Text(), line}
		}
		if err = scanner.Err(); err != nil {
			stderr <- logLine{command, err.Error(), line}
			log.Println("Error:", err)
		}
		stream.Close()
	}(errStream)

	//defer log.Println("closing execute")

	err = e.cmd.Run()
	if err != nil {
		atomic.AddUint32(&line, 1)
		stderr <- logLine{command, err.Error(), line}
		return
	}
	atomic.AddUint32(&line, 1)
	stdout <- logLine{command, "exit status 0", line}

}

func (e *executor) stop() {
	if e.cmd == nil || e.cmd.Process == nil {
		return
	}

	// try to terminate
	group, err := os.FindProcess(-1 * e.cmd.Process.Pid)
	if err != nil {
		log.Println("Error finding pid:", err)
		return
	}
	err = group.Signal(syscall.SIGTERM)
	if err != nil {
		log.Println("Error terminating process:", err)
		return
	}
	if e.cmd.Process == nil {
		log.Println("Terminated process:", e.cmd.Process.Pid)
		return
	}

	// try to kill
	group, err = os.FindProcess(-1 * e.cmd.Process.Pid)
	if err != nil {
		log.Println("Error finding pid:", err)
		return
	}
	err = group.Signal(syscall.SIGKILL)
	if err != nil {
		log.Println("Error killing process:", err)
		return
	}
	log.Println("Killed process:", e.cmd.Process.Pid)
}
