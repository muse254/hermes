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

	songs := make(chan int, 1)

	// a bit quirky 😞
	signal.Notify(j.interrupt, os.Interrupt)
	errLogger(notify.Watch(*projectDir, j.watch, notify.All))

	fmt.Printf("hermes: %s %s ...\n", executing, projectName)
	errLogger(j.cmd.Start())

	// BUG. Goroutine returns.
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

	// waits for program execute
	// watches for changes
	// listens for a SIGINT
	select {
	// This case has a BUG
	case <-j.watch:
		newWG := sync.WaitGroup{}
		newWG.Add(1)
		// playLyre while waiting for all changes to be aggregated,
		// write number of changes recorded to songs 😷

		defer newWG.Done()
		playLyre(3*time.Second, songs)
		select {
		case <-j.interrupt:
			fmt.Println("\nhermes has received SIGINT")
			os.Exit(0)
		// dequeue, enqueue
		case changes := <-songs:
			songs <- changes
		}
		fmt.Printf("\nhermes: %d changes on %s\n", <-songs, projectName)

	// this case works perfectly
	case <-j.interrupt:
		j.closeWait <- true
		kill(j.cmd.Process)
		fmt.Printf("\n%s has received SIGINT\n", projectName)

		fmt.Printf("\nhermes waiting for changes on %s\n", projectName)
		select {
		// dequeue, quirk
		case someChange := <-j.watch:
			// enqueued
			j.watch <- someChange
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

// todo
// playLyre aggregates changes with a time difference of
// _ seconds and identifies them as a single change
func playLyre(s time.Duration, songs chan int) {

	group := sync.WaitGroup{}

	done := make(chan bool, 1)
	song := make(chan notify.EventInfo, 1)
	var count int

	errLogger(notify.Watch(*projectDir, song, notify.All))

	// setting the timeout
	group.Add(1)
	go func() {
		defer group.Done()
		timer := time.NewTimer(s)
		<-timer.C
		done <- true
	}()

	// goroutine waiting for changes & timeout
	group.Add(1)
	go func() {
		defer group.Done()
		select {
		case <-song:
			count++
		case <-done:
			close(song)
			return
		}
	}()

	group.Wait()
	songs <- count
}

func errLogger(err error) {
	if err != nil {
		log.Fatal(err)
	}
}

// MAJOR BUG
// making changes while running a process leaks the previous process's resources
