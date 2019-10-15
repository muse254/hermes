package main

import (
	"flag"
	"fmt"
	"github.com/rjeczalik/notify"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
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
	toWait = flag.Int("wait", 5,
		"time in seconds to wait for next change before re-execution of command")

	wd = flag.Bool("wd", false,
		"it assumes the current parent directory as the project's directory")
)

// journey wraps the necessary channels to be used
// during the project's execution lifetime on a hermes instance
type journey struct {
	interrupt chan os.Signal
	watch     chan notify.EventInfo
	wait      chan error
	killWait  chan bool
}

func main() {
	// help is the help message on usage of hermes
	var help = "USAGE: ./hermes -project=path/to/some/projectFolder -gorun\n" +
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

	if *projectDir == "" && *wd == true {
		dir, err := os.Getwd()
		errLogger(err)
		*projectDir = dir
	} else if *projectDir == "" {
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

	watch := make(chan notify.EventInfo, 1)
	interrupt := make(chan os.Signal, 1)
	wait := make(chan error, 1)
	killWait := make(chan bool, 1)

	hermes := &journey{
		interrupt,
		watch,
		wait,
		killWait,
	}

	if *gorun {
		hermes.carryMessage("run")
	} else if *gotest {
		hermes.carryMessage("test")
	} else if *gobuild {
		hermes.carryMessage("build")
	}

}

// carryMessage handles the child process execution, termination and re-execution as needed
// by directory changes made and SIGINT
func (j *journey) carryMessage(execute string) {
	var executing string

	subs := strings.SplitN(*projectDir, "/", -1)
	projectName := subs[len(subs)-1]

	errLogger(notify.Watch(*projectDir, j.watch, notify.All))
	signal.Notify(j.interrupt, os.Interrupt)
	for {
		/*
			Pressing Enter key means that Hermes does a re-execution of the program
		*/
		var cmd *exec.Cmd
		switch execute {
		case "run":
			executing = "running"
			// go build && ./projectName works
			build := message("go", "build")
			err := build.Run()
			errLogger(err)
			if runtime.GOOS == "windows" {
				cmd = message(projectName)
			} else {
				cmd = message("./" + projectName)
			}
		case "build":
			executing = "building"
			cmd = message("go", "build")
		case "test":
			executing = "testing"
			cmd = message("go", "test")
		}
		fmt.Printf("hermes: %s %s ...\n", executing, projectName)
		go func() {
			j.wait <- cmd.Run()
		}()

		stdin := make(chan int)
		go func() {
			buff := make([]byte, 2)
			sth, _ := os.Stdin.Read(buff)
			stdin <- sth

		}()

		changesSum := make(chan int, 1)
		select {
		case <-stdin:
			// cleanup
			fmt.Println("stdin: RE-EXECUTION")
			kill(cmd.Process)
			err := <-j.wait
			if exitErr, ok := err.(*exec.ExitError); ok {
				fmt.Printf("\n%s: exit code %d\n", projectName, exitErr.ExitCode())
			}

		case <-j.watch:
			// playLyre while waiting for all changes to be aggregated,
			// write number of changes to changesSum
			go playLyre(j.watch, changesSum, true)
			select {
			case <-stdin:
				// when you don't want to wait for the say 5 seconds
				fmt.Println("stdin: RE-EXECUTION")
				kill(cmd.Process)
				err := <-j.wait
				if exitErr, ok := err.(*exec.ExitError); ok {
					fmt.Printf("\n%s: exit code %d\n", projectName, exitErr.ExitCode())
				}
			case <-j.interrupt:
				fmt.Println("\nhermes has received SIGINT")
				os.Exit(0)
			case changes := <-changesSum:
				fmt.Printf("\nhermes: %d change(s) on %s\n", changes, projectName)
				kill(cmd.Process)
				err := <-j.wait
				if exitErr, ok := err.(*exec.ExitError); ok {
					fmt.Printf("\n%s: exit code %d\n\n", projectName, exitErr.ExitCode())
				}
			}

		case <-j.interrupt:
			kill(cmd.Process)
			err := <-j.wait
			if exitErr, ok := err.(*exec.ExitError); ok {
				fmt.Printf("\n%s: exit code %d", projectName, exitErr.ExitCode())
			}
			fmt.Printf("\nhermes waiting for changes on %s\n", projectName)
			// aggregate changes if any
			go playLyre(j.watch, changesSum, false)
			select {
			case <-stdin:
				fmt.Println("stdin: RE-EXECUTION")
			case changes := <-changesSum:
				fmt.Printf("\nhermes: %d change(s) on %s\n", changes, projectName)
			case <-j.interrupt:
				fmt.Println("\nhermes has received SIGINT")
				os.Exit(0)
			}

		case err := <-j.wait:
			if err == nil {
				fmt.Printf("hermes: %s %s was successful\n", projectName, execute)
			} else {
				if exitError, ok := err.(*exec.ExitError); ok {
					code := exitError.ExitCode()
					// program error: 1, shut by signal: -1
					fmt.Printf("hermes: %s %s was unsuccessful, exit code %d\n", projectName, execute, code)
				}
			}
			// aggregate changes if any
			go playLyre(j.watch, changesSum, false)
			fmt.Printf("\nhermes waiting for changes on %s\n", projectName)
			select {
			case <-stdin:
				fmt.Println("stdin: RE-EXECUTION")
			case changes := <-changesSum:
				fmt.Printf("\nhermes: %d change(s) on %s\n", changes, projectName)
			case <-j.interrupt:
				fmt.Println("\nhermes has received SIGINT")
				os.Exit(0)
			}
		}
	}
}

// kill terminates the program by sending SIGKILL and releasing resources
// associated with the initial execution
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
func message(name string, arg ...string) (cmd *exec.Cmd) {
	cmd = exec.Command(name, arg...)
	cmd.Stdout = os.Stdout
	cmd.Stdin = os.Stdin
	cmd.Dir = *projectDir
	cmd.Stderr = os.Stderr
	return
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
		fmt.Printf("\nhermes aggregating changes\n")
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

// updateExe takes in the "projects" path, looks for projects that have hermes executable
// in the even of a new build, the executable in the projects are updated
func updateExe(projectsPath string) error  {
	projectsFolder, err := os.Open(projectsPath)
	errLogger(err)

	projects, err := projectsFolder.Readdirnames(0)
	errLogger(err)

	for _, project := range projects{
		project = projectsPath + "/" + project
		projectFolder, err := os.Open(project)
		errLogger(err)

		files, err := projectFolder.Readdirnames(0)
		for _, file := range files{
			if file == "hermes"{

			}
		}
	}
	return nil
}

func errLogger(err error) {
	if err != nil {
		log.Fatal(err)
	}
}
