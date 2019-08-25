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

	// I have not come round to using this
	// todo: check for race conditions
	mutex   sync.Mutex
}

func main() {
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

	// use gorun = true as default value
	if *gorun == false && *gotest == false && *gobuild == false {
		*gorun = true
	}

	subs := strings.SplitN(*projectDir, "/", -1)
	projectName := subs[len(subs)-1]

	for {
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
				pre,
				sync.Mutex{},
			}

			wg.Add(1)
			go func() {
				defer wg.Done()
				fmt.Printf("hermes: running %s ...\n", projectName)
				errLogger(proc.command.Start())

				// waits for process to complete.
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

				select {
				case <-watch:
					closeWait <- true
					clean(wait,proc)
					return
				case <-interrupt:
					closeWait <- true
					clean(wait,proc)
					fmt.Printf("\n%s has received SIGINT\n",projectName)
					newWG := sync.WaitGroup{}

					newWG.Add(1)
					// listen or die
					go func() {
						defer newWG.Done()
						select {
							// dequeue, quirk
						case someChange := <-watch:
							// enqueued
							watch <- someChange
						case <-interrupt:
							fmt.Println("\n hermes has received SIGINT")
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
				}
			}()
			wg.Wait()
			log.Printf("\n\nhermes: rerunning %s ...\n", projectName)

		} else if *gotest {
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
			//todo
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

// clean terminates the program by sending a SIGKILL
// it also releases resources.
//
// !!! process.Release leaks resources after parent process has returned
func clean(wait chan error,proc *process) {
	err := proc.command.Process.Kill()
	if err != nil{
		_,_ =fmt.Fprintf(os.Stdout,err.Error())
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

//
func errLogger(err error) {
	if err != nil {
		log.Fatal(err)
	}
}

// BUGS
// processes leak after hermes has returned
// should release resources of previous os.Process
