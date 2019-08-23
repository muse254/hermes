package main

import (
	"flag"
	"github.com/rjeczalik/notify"
	"log"
	"os"
	"os/exec"
)

var projectDir = flag.String("project", "",
	"it point to the project directory to watch for changes")
var mainPath = flag.String("main.go file", "",
	"it points to the main.go file from where the program is run")

func main() {
	flag.Parse()
	if *projectDir == ""{
		flag.PrintDefaults()
		return
	}
	if *mainPath == ""{
		dir, err := getMainPath()
		errLogger(err)
		*mainPath = dir
	}

	listen := make(chan notify.EventInfo, 1)
	defer close(listen)
	for {
		pre := run()
		singleEvent(listen)
		<-listen
		errLogger(pre.Kill())
	}
}

func getMainPath() (path string, err error)  {

}

//
func singleEvent(listen chan notify.EventInfo) {
	errLogger(notify.Watch(*projectDir, listen, notify.All))
}

func run() *os.Process {
	cmd := exec.Command("go", "run", *mainPath)
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
