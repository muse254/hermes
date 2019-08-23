package main

import (
	"flag"
	"fmt"
	"github.com/rjeczalik/notify"
	"log"
	"os"
	"os/exec"
	"strings"
)

var projectDir = flag.String("project", "",
	"it points to the project directory to watch for changes")

func main() {
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

	if *gorun == false && *gotest == false && *gobuild == false {
		*gorun = true
	}

	listen := make(chan notify.EventInfo, 1)
	defer close(listen)

	subs := strings.SplitN(*projectDir, "/", -1)
	projectName := subs[len(subs)-1]
	for {
		if *gorun {
			errLogger(notify.Watch(*projectDir, listen, notify.All))
			pre := cmd("go", "run", *mainPath)
			go func() {
				fmt.Printf("hermes: running %s",projectName)
				errLogger(pre.Start())
				pre.Wait()
			}()
			<-listen
			err := pre.Process.Signal(os.Kill)
			errLogger(err)
			log.Println("hermes: rerunning...")
		} else if *gotest {
			pre := cmd("go", "test")
			errLogger(notify.Watch(*projectDir, listen, notify.All))
			go func() {
				fmt.Printf("hermes: testing %s",projectName)
				errLogger(pre.Start())
				pre.Wait()
			}()
			<-listen
			err := pre.Process.Signal(os.Kill)
			errLogger(err)
			log.Println("hermes: retesting...")
		} else if *gobuild {
			//todo
			pre := cmd("go", "build")
			errLogger(notify.Watch(*projectDir, listen, notify.All))
			go func() {
				fmt.Printf("hermes: building %s",projectName)
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
func cmd(name string, arg ...string) *exec.Cmd {
	cmd := exec.Command(name, arg...)
	cmd.Stdout = os.Stdout
	cmd.Dir = *projectDir
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