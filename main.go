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
	process  *exec.Cmd
	mutex      sync.Mutex
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

	// use gorun as default value
	if *gorun == false && *gotest == false && *gobuild == false {
		*gorun = true
	}

	subs := strings.SplitN(*projectDir, "/", -1)
	projectName := subs[len(subs)-1]

	//errInterrupt := errors.New(fmt.Sprintf("%s has received SIGINT",projectName))

	wg := sync.WaitGroup{}
	listen := make(chan notify.EventInfo, 1)
	defer close(listen)
	interrupt := make(chan os.Signal, 1)
	defer close(interrupt)
	wait := make(chan error, 1)
	defer close(wait)

	for {
		if *gorun {
			errLogger(notify.Watch(*projectDir, listen, notify.All))
			signal.Notify(interrupt, os.Interrupt)
			pre := cmd(*projectDir, "go", "run", *mainPath)
			proc := &process{
				 pre,
			    sync.Mutex{},
			}

			wg.Add(1)
			go func() {

				defer wg.Done()
				fmt.Printf("hermes: running %s ...\n", projectName)
				errLogger(proc.process.Start())

				//go procWait(wait, proc)

				select {
				case <-listen:
					clean(proc)
					break
				case <-interrupt:
					clean(proc)
					break
				case err := <-wait:
					if err == nil {
						fmt.Printf("hermes: %s run was successful\n", projectName)
					} else {
						if exitError, ok := err.(*exec.ExitError); ok {
							code := exitError.ExitCode()
							fmt.Printf("hermes: %s run was unsuccessful, exit code %d\n", projectName, code)
						}
					}
					break
				}
				return
			}()
			wg.Wait()
			log.Printf("hermes: rerunning %s ...\n", projectName)

		} else if *gotest {
			pre := cmd(*projectDir, "go", "test")
			errLogger(notify.Watch(*projectDir, listen, notify.All))
			go func() {
				fmt.Printf("hermes: testing %s\n", projectName)
				errLogger(pre.Start())
				pre.Wait()
			}()
			<-listen
			err := pre.Process.Signal(os.Kill)
			errLogger(err)
			log.Println("hermes: retesting...")
		} else if *gobuild {
			//todo
			pre := cmd(*projectDir, "go", "build")
			errLogger(notify.Watch(*projectDir, listen, notify.All))
			go func() {
				fmt.Printf("hermes: building %s\n", projectName)
				errLogger(pre.Start())
				pre.Wait()
				// run the build: ./projectName
				cmd("", projectName)
			}()
			<-listen
			err := pre.Process.Signal(os.Kill)
			errLogger(err)
			log.Println("hermes: rebuilding")
		}

	}
}

// procWait waits for the process to complete, If it completed
// without interrupt being called it writes to the chan error
func procWait(err chan error, proc *process) {
	someErr := proc.process.Wait()
	err <- someErr
}

// clean releases resources of the process and then does a Kill
func clean(proc *process)  {
	proc.mutex.Lock()
	proc.process.Process.Release()
	proc.process.Process.Kill()
	proc.mutex.Unlock()
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
