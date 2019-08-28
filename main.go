package main

import (
	"flag"
	"fmt"
	"github.com/rjeczalik/notify"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"sync"
	"time"
)

var (
	// projectDir points to the project directory to watch for changes
	projectDir = flag.String("project", "",
		"it points to the project directory to watch for changes")
	// mainPath provides the path to the .go main file
	mainPath = flag.String("main", "",
		"it points to the main.go file from where the program is run")
)

// wingedSandals wraps the necessary channels and command to be used
// during the project's execution lifetime on a hermes instance
type wingedSandals struct {
	cmd       *exec.Cmd
	interrupt chan os.Signal
	watch     chan notify.EventInfo
	wait      chan error
	closeWait chan bool
}

func main() {
	// help is the help wingedSandals on usage of hermes
	var help = "USAGE: ./hermes -project=ProjectDirectory -gorun\n" +
		"Hermes reruns or rebuilds or retests your project every time a saved change is made\n" +
		"in your project directory\n"

	// gorun does 'go run'
	gorun := flag.Bool("gorun", false,
		"if true it does a 'go run ...' for every change made")
	// gotest does a 'go test'
	gotest := flag.Bool("gotest", false,
		"if true it does a 'go test' for every change made")
	// gobuild does a 'go build'
	gobuild := flag.Bool("gobuild", false,
		"if true does a 'go build' for every change made")
	flag.Parse()

	if *projectDir == "" {
		_, _ = fmt.Fprintf(os.Stderr, help)
		flag.PrintDefaults()
		return
	}
	if *mainPath == "" {
		filePath, err := lookForMain(*projectDir)
		if err != nil {
			errLogger(err)
		}
		*mainPath = filePath
	}

	// only a single execution flag should be true
	if *gorun == true && *gobuild == true || *gorun == true && *gotest == true || *gotest == true && *gobuild == true {
		_, _ = fmt.Fprintf(os.Stderr, help)
		flag.PrintDefaults()
		return
	}

	// use gorun = true as default value
	if *gorun == false && *gotest == false && *gobuild == false {
		*gorun = true
	}

	for {
		// created in each iteration. To optimize later?
		watch := make(chan notify.EventInfo, 1)
		interrupt := make(chan os.Signal, 1)
		wait := make(chan error, 1)
		closeWait := make(chan bool, 1)

		if *gorun {
			run := &wingedSandals{
				message(*projectDir, "go", "run", *mainPath),
				interrupt,
				watch,
				wait,
				closeWait,
			}
			run.carryMessage("run")

		} else if *gotest {
			test := &wingedSandals{
				message(*projectDir, "go", "test"),
				interrupt,
				watch,
				wait,
				closeWait,
			}
			test.carryMessage("test")

		} else if *gobuild {
			build := &wingedSandals{
				message(*projectDir, "go", "build"),
				interrupt,
				watch,
				wait,
				closeWait,
			}
			build.carryMessage("build")
		}
	}
}

// carryMessage handles the child process execution, termination and re-execution as needed
// by directory changes made and SIGINT
func (m *wingedSandals) carryMessage(execute string) {

	// this looks dumb ikr 😆
	var executing string
	switch execute {
	case "run":
		executing = "running"
	case "build":
		executing = "building"
	case "test":
		executing = "testing"
	}

	wg := sync.WaitGroup{}

	subs := strings.SplitN(*projectDir, "/", -1)
	projectName := subs[len(subs)-1]

	// a bit quirky 😞
	signal.Notify(m.interrupt, os.Interrupt)
	errLogger(notify.Watch(*projectDir, m.watch, notify.All))

	wg.Add(1)
	go func() {
		defer wg.Done()
		fmt.Printf("hermes: %s %s ...\n", executing, projectName)
		errLogger(m.cmd.Start())

		// goroutine waits for process to complete or for wait chan to be closed
		go func() {
			select {
			case m.wait <- m.cmd.Wait():
				close(m.closeWait)
				return
			case <-m.closeWait:
				close(m.wait)
				return
			}
		}()

		// waits for program execute
		// watches for changes
		// listens for a SIGINT
		select {
		case <-m.watch:
			m.closeWait <- true
			kill(m.cmd.Process)
			return
		case <-m.interrupt:
			m.closeWait <- true
			kill(m.cmd.Process)
			fmt.Printf("\n%s has received SIGINT\n", projectName)

			newWG := sync.WaitGroup{}
			newWG.Add(1)
			// listen or die
			go func() {
				defer newWG.Done()
				fmt.Printf("\nhermes waiting for changes on %s\n", projectName)
				select {
				// dequeue, quirk
				case someChange := <-m.watch:
					// enqueued
					m.watch <- someChange
				case <-m.interrupt:
					fmt.Println("\nhermes has received SIGINT")
					pState, _ := m.cmd.Process.Wait()
					fmt.Printf("\n%s exit status is %v", projectName, pState.Exited())

					os.Exit(0)
				}
			}()
			newWG.Wait()
		case err := <-m.wait:
			if err == nil {
				fmt.Printf("hermes: %s %s was successful, exit code 0\n", projectName, execute)
			} else {
				if exitError, ok := err.(*exec.ExitError); ok {
					code := exitError.ExitCode()
					// program error: 1, shut by signal: -1
					fmt.Printf("hermes: %s %s was unsuccessful, exit code %d\n", projectName, execute, code)
				}
			}

			// watch for file changes or SIGINT
			newWG := sync.WaitGroup{}
			newWG.Add(1)
			go func() {
				defer newWG.Done()
				fmt.Printf("\nhermes waiting for changes on %s\n", projectName)
				select {
				case someChange := <-m.watch:
					m.watch <- someChange
				case <-m.interrupt:
					fmt.Println("\nhermes has received SIGINT")
					os.Exit(0)
				}

			}()
			newWG.Wait()
		}
	}()
	wg.Wait()
	log.Printf("\n\nhermes: re%s %s ...\n", executing, projectName)
}

// kill terminates the program by sending a SIGKILL
func kill(proc *os.Process) {
	err := proc.Kill()
	if err != nil {
		_, _ = fmt.Fprintf(os.Stdout, err.Error())
	}
	err = proc.Release()
	if err != nil {
		_, _ = fmt.Fprintf(os.Stdout, err.Error())
	}
}

// lookForMain looks for main.go file in the directory given
// returning the main file if it was found and err, if any
func lookForMain(path string) (mainFile string, err error) {
	folder, err := os.Open(path)
	if err != nil {
		return "", nil
	}

	files, err := folder.Readdirnames(0)
	if err != nil {
		return "", nil
	}

	for _, file := range files {
		if file == "main.go" {
			return path + "/" + file, nil
		}
		thisFile, err := os.Open(path + "/" + file)
		if err != nil {
			return "", nil
		}
		thisFileInfo, err := thisFile.Stat()
		if err != nil {
			return "", nil
		}
		if thisFileInfo.IsDir() {
			mainFile, err = lookForMain(path + "/" + file)
			if err != nil {
				return "", nil
			}
		}
	}
	return
}

// message initialises the command
func message(projectDir string, name string, arg ...string) *exec.Cmd {
	cmd := exec.Command(name, arg...)
	cmd.Stdout = os.Stdout
	cmd.Stdin = os.Stdin
	cmd.Dir = projectDir
	cmd.Stderr = os.Stderr

	return cmd
}

// todo: treat changes within a given time diff. as a single change?
// playLyre aggregates changes that span a time difference of
// _ seconds between two changes in the project and takes a chan int to write the
// number of changes to. If it waits after _ seconds the next change is treated as
// a new different set of changes
func playLyre(watch chan notify.EventInfo, s time.Duration, single chan int) {
	var stop, reset chan bool
	dealer := func() {
		select {
		case <-watch:
		}
	}

	// listen for a single change
	var changes int
	for {
		go dealer()
		select {
		case <-reset:
			break
		case <-stop:
			single <- changes
			return
		}
		changes++
	}

}

func errLogger(err error) {
	if err != nil {
		log.Fatal(err)
	}
}
