/* File: public_output_service.go

Watches 24/7 for outbound files, and when files arrive
it instantly sends them to the clients sftp server
using a child process expect script to run sftp

*/

package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/smtp"
	"os"
	"os/exec"
	"path"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/blackjack/syslog"
	"github.com/fsnotify/fsnotify"
)

var (
	debug     = flag.Bool("v", false, "Enable verbose debugging output")
	term      = flag.Bool("t", false, "Just run in the terminal (instead of an acme win)")
	exclude   = flag.String("x", "", "Exclude files and directories matching this regular expression")
	watchPath = flag.String("p", ".", "The path to watch")
)

var excludeRe *regexp.Regexp

const rebuildDelay = 200 * time.Millisecond
const customeremail = "customer@cloud3000.com"
const ediadminemail = "edimgr@cloud3000.com"

// The name of the syscall.SysProcAttr.Setpgid field.
const setpgidName = "Setpgid"

var (
	hasSetPGID bool
	killChan   = make(chan time.Time, 1)
)

type ui interface {
	redisplay(func(io.Writer))
	// An empty struct is sent when the command should be rerun.
	rerun() <-chan struct{}
}

type writerUI struct{ io.Writer }

func (w writerUI) redisplay(f func(io.Writer)) { f(w) }

func (w writerUI) rerun() <-chan struct{} { return nil }

func _ediEMAIL(mailfrom string, mailto string, mailsub string, mailmsg string) int {
	// Set up authentication information.
	auth := smtp.PlainAuth("", "michael@cloud3000.com",
		"***passwd***",
		"smtpsrvr.com")

	// Connect to the server, authenticate, set the sender and recipient,
	// and send the email all in one step.
	to := []string{mailto}
	msg := []byte("To: " + mailto + "\r\n" +
		"From: " + mailfrom + "\r\n" +
		"Subject: " + mailsub + "\r\n" +
		"\r\n" +
		mailmsg + "\r\n")
	err := smtp.SendMail("smtpsrvr.com:587", auth, "michael@cloud3000.com", to, msg)
	if err != nil {
		fmt.Printf("%v\n", err)
	}
	return 0
}

func main() {
	syslog.Openlog("public_output_service", syslog.LOG_PID, syslog.LOG_USER)
	syslog.Syslog(syslog.LOG_INFO, "public_output_service started")
	defer syslog.Syslog(syslog.LOG_INFO, "public_output_service ended")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage of %s: [flags] command [command args…]\n", os.Args[0])
		flag.PrintDefaults()
	}
	flag.Parse()

	t := reflect.TypeOf(syscall.SysProcAttr{})
	f, ok := t.FieldByName(setpgidName)
	if ok && f.Type.Kind() == reflect.Bool {
		debugPrint("syscall.SysProcAttr.Setpgid exists and is a bool")
		hasSetPGID = true
	} else if ok {
		debugPrint("syscall.SysProcAttr.Setpgid exists but is a %s, not a bool", f.Type.Kind())
	} else {
		debugPrint("syscall.SysProcAttr.Setpgid does not exist")
	}

	if flag.NArg() == 0 {
		flag.Usage()
		os.Exit(1)
	}

	myui := ui(writerUI{os.Stdout})

	timer := time.NewTimer(0)
	changes := startWatching(*watchPath)
	lastRun := time.Time{}
	lastChange := time.Now()

	for {
		select {
		case lastChange = <-changes:
			timer.Reset(rebuildDelay)

		case <-myui.rerun():
			lastRun = run(myui)

		case <-timer.C:
			if lastRun.Before(lastChange) {
				lastRun = run(myui)
			}
		}
	}
}

func run(myui ui) time.Time {
	myui.redisplay(func(out io.Writer) {
		cmd := exec.Command(flag.Arg(0), flag.Args()[1:]...)
		cmd.Stdout = out
		cmd.Stderr = out
		if hasSetPGID {
			var attr syscall.SysProcAttr
			reflect.ValueOf(&attr).Elem().FieldByName(setpgidName).SetBool(true)
			cmd.SysProcAttr = &attr
		}
		io.WriteString(out, "Args: "+strings.Join(flag.Args(), " ")+"\n")
		start := time.Now()
		if err := cmd.Start(); err != nil {
			io.WriteString(out, "fatal: "+err.Error()+"\n")
			os.Exit(1)
		}
		if s := wait(start, cmd); s != 0 {
			io.WriteString(out, "exit status "+strconv.Itoa(s)+"\n")
		}
		io.WriteString(out, time.Now().String()+"\n")
	})

	return time.Now()
}

func wait(start time.Time, cmd *exec.Cmd) int {
	var n int
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case t := <-killChan:
			if t.Before(start) {
				continue
			}
			p := cmd.Process.Pid
			if hasSetPGID {
				p = -p
			}
			if n == 0 {
				debugPrint("Sending SIGTERM")
				syscall.Kill(p, syscall.SIGTERM)
			} else {
				debugPrint("Sending SIGKILL")
				syscall.Kill(p, syscall.SIGKILL)
			}
			n++

		case <-ticker.C:
			var status syscall.WaitStatus
			p := cmd.Process.Pid
			switch q, err := syscall.Wait4(p, &status, syscall.WNOHANG, nil); {
			case err != nil:
				panic(err)
			case q > 0:
				cmd.Wait() // Clean up any goroutines created by cmd.Start.
				return status.ExitStatus()
			}
		}
	}
}

func kill() {
	select {
	case killChan <- time.Now():
		debugPrint("Killing")
	}
}

func startWatching(p string) <-chan time.Time {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		panic(err)
	}

	switch isdir, err := isDir(p); {
	case err != nil:
		log.Fatalf("Failed to watch %s: %s", p, err)
	case isdir:
		watchDir(w, p)
	default:
		watch(w, p)
	}

	changes := make(chan time.Time)

	go sendChanges(w, changes)

	return changes
}

func fcheck(e error) {
	if e != nil {
		io.WriteString(os.Stdout, "fatal: "+e.Error()+"\n")
		panic(e)
	}
}

func sftpScript(fname string) string {
	myext := path.Ext(fname)
	myfile := strings.Replace(path.Base(fname), myext, ".exp", 4)
	fmt.Printf("Script File: %s\n", myfile)
	syslog.Syslogf(syslog.LOG_INFO, "Output File: %s\n", fname)
	syslog.Syslogf(syslog.LOG_INFO, "Script File: %s\n", myfile)
	os.Remove(myfile)

	f, oerr := os.Create(myfile)
	fcheck(oerr)
	_, ferr := f.WriteString("#!/usr/bin/expect -f\n")
	fcheck(ferr)
	_, ferr = f.WriteString("set force_conservative 0\n")
	fcheck(ferr)
	_, ferr = f.WriteString("if {$force_conservative} {\n")
	fcheck(ferr)
	_, ferr = f.WriteString("        set send_slow {1 .1}\n")
	fcheck(ferr)
	_, ferr = f.WriteString("        proc send {ignore arg} {\n")
	fcheck(ferr)
	_, ferr = f.WriteString("                sleep .1\n")
	fcheck(ferr)
	_, ferr = f.WriteString("                exp_send -s -- $arg\n")
	fcheck(ferr)
	_, ferr = f.WriteString("        }\n")
	fcheck(ferr)
	_, ferr = f.WriteString("}\n")
	fcheck(ferr)
	_, ferr = f.WriteString("set timeout -1\n")
	fcheck(ferr)
	_, ferr = f.WriteString("spawn $env(SHELL)\n")
	fcheck(ferr)
	_, ferr = f.WriteString("send -- \"PS1='$ '\r\"\n")
	fcheck(ferr)
	_, ferr = f.WriteString("match_max 100000\n")
	fcheck(ferr)
	_, ferr = f.WriteString("expect  \"$ \"\n")
	fcheck(ferr)
	_, ferr = f.WriteString("send -- \"sftp username@customerdomain.com\\r\"\n")
	fcheck(ferr)
	_, ferr = f.WriteString("expect \"password: \"\n")
	fcheck(ferr)
	_, ferr = f.WriteString("send -- \"password\\r\"\n")
	fcheck(ferr)
	_, ferr = f.WriteString("expect \"sftp> \"\n")
	fcheck(ferr)
	_, ferr = f.WriteString("send -- \"cd dir1\r\"\n")
	fcheck(ferr)
	_, ferr = f.WriteString("expect  \"sftp> \"\n")
	fcheck(ferr)
	_, ferr = f.WriteString("send -- \"cd dir2\r\"\n")
	fcheck(ferr)
	_, ferr = f.WriteString("expect  \"sftp> \"\n")
	fcheck(ferr)
	_, ferr = f.WriteString("send -- \"put " + fname + "\\r\"\n")
	fcheck(ferr)
	_, ferr = f.WriteString("expect  \"sftp> \"\n")
	fcheck(ferr)
	_, ferr = f.WriteString("send -- \"quit\\r\"\n")
	fcheck(ferr)
	_, ferr = f.WriteString("expect \"$ \"\n")
	fcheck(ferr)
	_, ferr = f.WriteString("send -- \"exit\\r\"\n")
	fcheck(ferr)
	_, ferr = f.WriteString("expect eof\n")
	fcheck(ferr)
	f.Sync()
	f.Close()
	ferr = os.Chmod(myfile, 0554)
	fcheck(ferr)
	return myfile
}

func sendChanges(w *fsnotify.Watcher, changes chan<- time.Time) {
	for {
		select {
		case err := <-w.Errors:
			log.Fatalf("Watcher error: %s\n", err)

		case ev := <-w.Events:
			if excludeRe != nil && excludeRe.MatchString(ev.Name) {
				debugPrint("ignoring event for excluded %s", ev.Name)
				continue
			}
			etime, err := modTime(ev.Name)
			if err != nil {
				log.Printf("Failed to get event time: %s", err)
				continue
			}
			debugPrint("%s at %s", ev, etime)
			debugPrint(" Operation: %s ", ev.Op)
			if ev.Op&fsnotify.Write != 0 {
				io.WriteString(os.Stdout, "WRITE"+"\n")
			}
			if ev.Op&fsnotify.Chmod != 0 {
				io.WriteString(os.Stdout, "CHMOD"+"\n")
			}
			if ev.Op&fsnotify.Remove != 0 {
				io.WriteString(os.Stdout, "REMOVE"+"\n")
			}
			if ev.Op&fsnotify.Rename != 0 {
				io.WriteString(os.Stdout, "RENAME"+"\n")
			}
			if ev.Op&fsnotify.Create != 0 {
			}

			if ev.Op&fsnotify.Create != 0 {
				io.WriteString(os.Stdout, "CREATE"+"\n")
				switch isdir, err := isDir(ev.Name); {
				case err != nil:
					log.Printf("Couldn't check if %s is a directory: %s", ev.Name, err)
					continue

				case isdir:
					watchDir(w, ev.Name)
					continue
				}
				time.Sleep(1 * time.Second)
				myext := path.Ext(ev.Name)
				scriptfile := strings.Replace(path.Base(ev.Name), myext, ".exp", 4)
				if myext == ".xml" {
					c1 := exec.Command("expect", sftpScript(ev.Name))

					if err := c1.Start(); err != nil {
						io.WriteString(os.Stdout, "fatal: "+err.Error()+"\n")
						efrom := customeremail
						eto := ediadminemail
						esub := "[EDI] Response Transfer Error: "
						emsg := fmt.Sprintf(
							"Transfer Filename: %s\n\n"+
								"     sftp: username@customerdomain.com\n"+
								"Directory: /dir1/dir2\n"+
								"     Program Name: expect %s\n"+
								"      Start Error: %s\n"+
								"        Date Time: %s\n",
							ev.Name,
							scriptfile,
							err.Error(),
							time.Now().Format("2006-01-02 15:04:05"))
						_ediEMAIL(efrom, eto, esub, emsg)
						os.Exit(1)
					}
					if err := c1.Wait(); err != nil {
						io.WriteString(os.Stdout, "fatal: "+err.Error()+"\n")
						efrom := customeremail
						eto := ediadminemail
						esub := "[EDI] Response Transfer Error: "
						emsg := fmt.Sprintf(
							"Transfer Filename: %s\n\n"+
								"     sftp: username@customerdomain.com\n"+
								"Directory: /dir1/dir2\n"+
								"     Program Name: expect %s\n"+
								"     Return Error: %s\n"+
								"        Date Time: %s\n",
							path.Base(ev.Name),
							scriptfile,
							err.Error(),
							time.Now().Format("2006-01-02 15:04:05"))
						_ediEMAIL(efrom, eto, esub, emsg)
						os.Exit(1)
					}
					efrom := customeremail
					eto := ediadminemail
					esub := "[EDI] Response Transfer: "
					emsg := fmt.Sprintf(
						" Filename: %s\n\n"+
							"     sftp: username@customerdomain.com\n"+
							"Directory: /dir1/dir2\n"+
							"Date Time: %s\n"+
							"   Status: Transfer Completed Successfully.",
						path.Base(ev.Name),
						time.Now().Format("2006-01-02 15:04:05"))
					_ediEMAIL(efrom, eto, esub, emsg)
					os.Remove("./processed/" + path.Base(ev.Name))
					os.Rename(ev.Name, "./processed/"+path.Base(ev.Name))
					os.Remove(scriptfile)
				}
			}
			changes <- etime
		}
	}
}

func modTime(p string) (time.Time, error) {
	switch s, err := os.Stat(p); {
	case os.IsNotExist(err):
		q := path.Dir(p)
		if q == p {
			qerr := errors.New("Failed to find directory for " + p)
			return time.Time{}, qerr
		}
		return modTime(q)

	case err != nil:
		return time.Time{}, err

	default:
		return s.ModTime(), nil
	}
}

func watchDir(w *fsnotify.Watcher, p string) {
	ents, err := ioutil.ReadDir(p)
	switch {
	case os.IsNotExist(err):
		return

	case err != nil:
		log.Printf("Failed to watch %s: %s", p, err)
	}

	for _, e := range ents {
		sub := path.Join(p, e.Name())
		if excludeRe != nil && excludeRe.MatchString(sub) {
			debugPrint("excluding %s", sub)
			continue
		}
		switch isdir, err := isDir(sub); {
		case err != nil:
			log.Printf("Failed to watch %s: %s", sub, err)

		case isdir:
			watchDir(w, sub)
		}
	}

	watch(w, p)
}

func watch(w *fsnotify.Watcher, p string) {
	debugPrint("Watching %s", p)

	switch err := w.Add(p); {
	case os.IsNotExist(err):
		debugPrint("%s no longer exists", p)

	case err != nil:
		log.Printf("Failed to watch %s: %s", p, err)
	}
}

func isDir(p string) (bool, error) {
	switch s, err := os.Stat(p); {
	case os.IsNotExist(err):
		return false, nil
	case err != nil:
		return false, err
	default:
		return s.IsDir(), nil
	}
}

func debugPrint(f string, vals ...interface{}) {
	if *debug {
		log.Printf("DEBUG: "+f, vals...)
	}
}
