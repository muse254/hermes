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
)

type process struct {
	command *exec.Cmd
	// I haven't come round to using this
	// todo: check for race conditions if any
	mutex sync.Mutex
}

func main() {
	var help = "USAGE: ./hermes -project=ProjectDirectory -gorun\n" +
		"Hermes reruns or rebuilds or retests your project every time a saved change is made\n" +
		"in your project directory\n"

	projectDir := flag.String("project", "",
		"it points to the project directory to watch for changes")
	mainPath := flag.String("main", "",
		"it points to the main.go file from where the program is run")
	gorun := flag.Bool("gorun", false,
		"if true it does a 'go run ...' for every change made")
	gotest := flag.Bool("gotest", false,
		"if true it does a 'go test' for every change made")
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

	// only a single bool flag should be true
	if *gorun == true && *gobuild == true || *gorun == true && *gotest == true || *gotest == true && *gobuild == true {
		_, _ = fmt.Fprintf(os.Stderr, help)
		flag.PrintDefaults()
		return
	}

	// use gorun = true as default value
	if *gorun == false && *gotest == false && *gobuild == false {
		*gorun = true
	}

	subs := strings.SplitN(*projectDir, "/", -1)
	projectName := subs[len(subs)-1]

	for {

		// for each iteration are these data structures created?
		// todo: move outside for loop?
		wg := sync.WaitGroup{}

		watch := make(chan notify.EventInfo, 1)
		interrupt := make(chan os.Signal, 1)
		wait := make(chan error, 1)
		closeWait := make(chan bool, 1)

		if *gorun {
			// a bit quirky 😞
			signal.Notify(interrupt, os.Interrupt)
			errLogger(notify.Watch(*projectDir, watch, notify.All))
			pre := cmd(*projectDir, "go", "run", *mainPath)
			proc := &process{
				command: pre,
			}

			wg.Add(1)
			go func() {
				defer wg.Done()
				fmt.Printf("hermes: running %s ...\n", projectName)
				errLogger(proc.command.Start())

				// goroutine waits for process to complete or for wait chan to be closed
				go func() {
					select {
					case wait <- proc.command.Wait():
						close(closeWait)
						return
					case <-closeWait:
						close(wait)
						return
					}
				}()

				// waits for program execution
				// watches for changes
				// listens for a SIGINT
				select {
				case <-watch:
					closeWait <- true
					kill(proc)
					return
				case <-interrupt:
					closeWait <- true
					kill(proc)
					fmt.Printf("\n%s has received SIGINT\n", projectName)

					newWG := sync.WaitGroup{}
					newWG.Add(1)
					// listen or die
					go func() {
						defer newWG.Done()
						fmt.Printf("\nhermes waiting for changes on %s\n", projectName)
						select {
						// dequeue, quirk
						case someChange := <-watch:
							// enqueued
							watch <- someChange
						case <-interrupt:
							fmt.Println("\nhermes has received SIGINT")
							os.Exit(0)
						}
					}()
					newWG.Wait()
				case err := <-wait:
					if err == nil {
						fmt.Printf("hermes: %s run was successful, exit code 0\n", projectName)
					} else {
						if exitError, ok := err.(*exec.ExitError); ok {
							code := exitError.ExitCode()
							// program error: 1, shut by signal: -1
							fmt.Printf("hermes: %s run was unsuccessful, exit code %d\n", projectName, code)
						}
					}

					// watch for file changes or SIGINT
					newWG := sync.WaitGroup{}
					newWG.Add(1)
					go func() {
						defer newWG.Done()
						fmt.Printf("\nhermes waiting for changes on %s\n", projectName)
						select {
						case someChange := <-watch:
							watch <- someChange
						case <-interrupt:
							fmt.Println("\nhermes has received SIGINT")
							os.Exit(0)
						}

					}()
					newWG.Wait()
				}
			}()
			wg.Wait()
			log.Printf("\n\nhermes: rerunning %s ...\n", projectName)

		} else if *gotest {
			// todo gotest
			pre := cmd(*projectDir, "go", "test")
			errLogger(notify.Watch(*projectDir, watch, notify.All))
			go func() {
				fmt.Printf("hermes: testing %s\n", projectName)
				errLogger(pre.Start())
				pre.Wait()
			}()
			<-watch
			err := pre.Process.Signal(os.Kill)
			errLogger(err)
			log.Println("hermes: retesting...")
		} else if *gobuild {
			// todo gobuild
			pre := cmd(*projectDir, "go", "build")
			errLogger(notify.Watch(*projectDir, watch, notify.All))
			go func() {
				fmt.Printf("hermes: building %s\n", projectName)
				errLogger(pre.Start())
				pre.Wait()
				// run the build: ./projectName
				cmd("", projectName)
			}()
			<-watch
			err := pre.Process.Signal(os.Kill)
			errLogger(err)
			log.Println("hermes: rebuilding")
		}

	}
}

// kill terminates the program by sending a SIGKILL
// it also releases resources. Processes dont leak e.g.
// Port numbers are released for http.Servers
//
// !!! process.Release leaks child process resources after parent has returned
func kill(proc *process) {
	err := proc.command.Process.Kill()
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

// cmd initialises the command
func cmd(projectDir string, name string, arg ...string) *exec.Cmd {
	cmd := exec.Command(name, arg...)
	cmd.Stdout = os.Stdout
	cmd.Stdin = os.Stdin
	cmd.Dir = projectDir
	cmd.Stderr = os.Stderr

	return cmd
}

func errLogger(err error) {
	if err != nil {
		log.Fatal(err)
	}
}

// BUG
// Changes should be aggregated. Every single change
// creates spawns rerun that leaks resources save for
// the initial for the first change rerun