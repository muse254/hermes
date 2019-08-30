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
	// time in seconds to wait for next change before re-execution of command
	toWait = flag.Int("wait", 10,
		"time in seconds to wait for next change before re-execution of command")
)

// journey wraps the necessary channels and command to be used
// during the project's execution lifetime on a hermes instance
type journey struct {
	cmd       *exec.Cmd
	interrupt chan os.Signal
	watch     chan notify.EventInfo
	wait      chan error
	closeWait chan bool
}

func main() {
	// help is the help message on usage of hermes
	var help = "USAGE: ./hermes -project=ProjectDirectory -gorun\n" +
		"Hermes reruns or rebuilds or retests your project every time a saved change is made\n" +
		"in your project directory\n"

	// gorun does 'go run'
	gorun := flag.Bool("gorun", false,
		"if true it does a 'go run _.go' for every change made")
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
		filePath, err := wingedSandals(*projectDir)
		errLogger(err)
		*mainPath = filePath
	}

	// only a single execution flag should be true
	if *gorun == true && *gobuild == true || *gorun == true &&
		*gotest == true || *gotest == true && *gobuild == true {
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
			hermes := &journey{
				message(*projectDir, "go", "run", *mainPath),
				interrupt,
				watch,
				wait,
				closeWait,
			}
			hermes.carryMessage("run")

			// I have not got to the point of fully implementing or testing
			// gotest or gobuild. todo after getting done with gorun
		} else if *gotest {
			hermes := &journey{
				message(*projectDir, "go", "test"),
				interrupt,
				watch,
				wait,
				closeWait,
			}
			hermes.carryMessage("test")

		} else if *gobuild {
			hermes := &journey{
				message(*projectDir, "go", "build"),
				interrupt,
				watch,
				wait,
				closeWait,
			}
			hermes.carryMessage("build")
		}
	}
}

// carryMessage handles the child process execution, termination and re-execution as needed
// by directory changes made and SIGINT
func (j *journey) carryMessage(execute string) {

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

	subs := strings.SplitN(*projectDir, "/", -1)
	projectName := subs[len(subs)-1]

	// a bit quirky 😞
	signal.Notify(j.interrupt, os.Interrupt)
	errLogger(notify.Watch(*projectDir, j.watch, notify.All))

	fmt.Printf("hermes: %s %s ...\n", executing, projectName)
	errLogger(j.cmd.Start())

	// goroutine waits for process to complete or for wait chan to be closed
	go func() {
		select {
		case j.wait <- j.cmd.Wait():
			close(j.closeWait)
			return
		case <-j.closeWait:
			close(j.wait)
			return
		}
	}()

	songs := make(chan int, 1)
	select {
	// This case has a BUG
	case <-j.watch:
		go playLyre(j.watch, songs, true)
		j.closeWait <- true
		kill(j.cmd.Process)
		// playLyre while waiting for all changes to be aggregated,
		// write number of changes recorded to songs 😷
		select {
		// cleans up both parent and child processes
		case <-j.interrupt:
			fmt.Println("\nhermes has received SIGINT")
			os.Exit(0)
		case changes := <-songs:
			fmt.Printf("\nhermes: %d change(s) on %s\n", changes, projectName)
		}

	// this case works perfectly
	case <-j.interrupt:
		j.closeWait <- true
		kill(j.cmd.Process)
		fmt.Printf("\n%s has received SIGINT\n", projectName)
		fmt.Printf("\nhermes waiting for changes on %s\n", projectName)
		// aggregate changes if any
		go playLyre(j.watch, songs, false)
		select {
		case changes := <-songs:
			fmt.Printf("\nhermes: %d change(s) on %s\n", changes, projectName)
		case <-j.interrupt:
			fmt.Println("\nhermes has received SIGINT")
			os.Exit(0)
		}
	// this case works perfectly.
	case err := <-j.wait:
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
		fmt.Printf("\nhermes waiting for changes on %s\n", projectName)
		select {
		case someChange := <-j.watch:
			j.watch <- someChange
		case <-j.interrupt:
			fmt.Println("\nhermes has received SIGINT")
			os.Exit(0)
		}

	}

	log.Printf("\n\nhermes: re%s %s ...\n", executing, projectName)
}

// todo
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

// wingedSandals looks for main.go file in the directory given
// returning the main file if it was found and err, if any
func wingedSandals(path string) (mainFile string, err error) {
	folder, err := os.Open(path)
	if err != nil {
		return "", nil
	}

	files, err := folder.Readdirnames(0)
	if err != nil {
		return "", nil
	}

	// For most cases this should work
	for _, file := range files {
		if file == "main.go" {
			return path + "/" + file, nil
		}
	}

	for _, file := range files {
		thisFile, err := os.Open(path + "/" + file)
		if err != nil {
			return "", nil
		}
		thisFileInfo, err := thisFile.Stat()
		if err != nil {
			return "", nil
		}
		if thisFileInfo.IsDir() {
			mainFile, err = wingedSandals(path + "/" + file)
			if err != nil {
				return "", nil
			}
		}
	}
	return
}

// message initialises the executable command
func message(projectDir string, name string, arg ...string) *exec.Cmd {
	cmd := exec.Command(name, arg...)
	cmd.Stdout = os.Stdout
	cmd.Stdin = os.Stdin
	cmd.Dir = projectDir
	cmd.Stderr = os.Stderr

	return cmd
}

// playLyre aggregates changes with a time difference of s
// it then writes the number of changes to songs
func playLyre(player chan notify.EventInfo, songs chan int, initial bool) {
	done := make(chan bool)
	reset := make(chan bool)
	// initial change before playLyre was called
	count := 1

	wg := sync.WaitGroup{}
	// setting the timer
	wg.Add(1)
	go func() {
		defer wg.Done()
		var timer *time.Timer
		if !initial {
			<-player
			timer = time.NewTimer(time.Duration(*toWait) * time.Second)
		} else {
			timer = time.NewTimer(time.Duration(*toWait) * time.Second)
		}
		for {
			select {
			case <-timer.C:
				done <- true
				return
			case <-reset:
				timer.Reset(time.Duration(*toWait) * time.Second)
			}
		}
	}()

	// goroutine waiting for changes & timeout
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-done:
				return
			case <-player:
				count++
				reset <- true
			}
		}
	}()

	wg.Wait()
	songs <- count
}

func errLogger(err error) {
	if err != nil {
		log.Fatal(err)
	}
}

// MAJOR BUG
// making changes while running a process leaks the previous process's resources
