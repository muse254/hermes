# hermes

The program watches for file changes in the project directory given and runs or builds or tests the project
for every change made (notify.ALL). Works on Linux.

Still a work in progress.

Catchy name.
The program should help when using go's templating system in doing frontend work (why not automate this😭).
Or whatever else you may find it useful for.


It relies on "github.com/rjeczalik/notify" repo ❤ to watch for file changes.

## Usage

hermes being a commandline program uses flags:
    
    -project: to provide the project directory  (string flag)
    -wait: to provide the time in seconds to wait for next change, default is 5s (int flag)
    
    For execution flags which are bool flags only one can be used:    
    -gorun: to state you want a 'go run' execution  
    -gobuild: to state that you want a 'go build && ./projectName' execution
    -gotest: to state you want a 'go test' execution
    
    For some reason if your func main is in another file other than main.go:
    -main: to state where the path to the .go file that contains main  (string flag)

Note that this error:

    ./main.go:123:25: exitError.ExitCode undefined (type *exec.ExitError has no field or method ExitCode)

is flagged on go1.10.4, and I suppose earlier versions, you should use go1.12.5 and later versions.

## 
Any comments, suggestions, PRs, or code reviews are welcome.
Am learning lets learn together.

