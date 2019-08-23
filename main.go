package main

import (
	"errors"
	"flag"
	"github.com/rjeczalik/notify"
	"log"
	"os"
	"os/exec"
)

var (
	projectDir = flag.String("project", "",
		"it point to the project directory to watch for changes")
	mainPath = flag.String("main", "",
		"it points to the main.go file from where the program is run")
	gorun = flag.Bool("gorun", false,
		"if true it does a 'go run .../.../main.go for every change made")
	gotest = flag.Bool("gotest", false,
		"if true it does a 'go test' for every change made")
	gobuild = flag.Bool("gobuild", false,
		"if true does a 'go build' for every change made")
)

func main() {
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
		if filePath == "" {
			errLogger(errors.New("main.go file was not found"))
		}
		*mainPath = filePath
	}

	if *gorun == false && *gotest == false && *gobuild == false {
		*gorun = true
	}

	listen := make(chan notify.EventInfo, 1)
	defer close(listen)
	for {
		if *gorun {
			pre := cmd("go", "run", *mainPath)
			errLogger(notify.Watch(*projectDir, listen, notify.All))
			<-listen
			errLogger(pre.Kill())
		} else if *gotest {
			pre := cmd("go", "test")
			errLogger(notify.Watch(*projectDir, listen, notify.All))
			<-listen
			errLogger(pre.Kill())
		} else if *gobuild {
			//todo
			pre := cmd("go", "build", "")
			errLogger(notify.Watch(*projectDir, listen, notify.All))
			<-listen
			errLogger(pre.Kill())
		}

	}
}

// lookForMain takes a folder and looks for main.go file
// if successful it returns mainFile, nil else it returns "", dir (if any) or nil
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
			if mainFile != "" {
				return
			}
		}
	}
	return
}

func cmd(name string, arg ...string) *os.Process {
	cmd := exec.Command(name, arg...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	errLogger(cmd.Run())
	return cmd.Process
}

func errLogger(err error) {
	if err != nil {
		log.Fatal(err)
	}
}
