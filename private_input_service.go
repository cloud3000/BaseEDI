package main

/* File: private_input_service.go

Listens on multiple ports for request from the private network.

*/

import (
	"fmt"
	"net"
	"net/smtp"
	"os"
	"os/exec"
	"strconv"
	"time"

	"github.com/blackjack/syslog"
)

const (
	SERVER_HOST = "45.55.216.201" // This is localhost public IP.
	SERVER_TYPE = "tcp"
	MR_PORT     = "30771" // Incomming requests for Materail Receipts
	CMD_PORT    = "30772"
)

func ediEmail(mailfrom string, mailto string, mailsub string, mailmsg string) int {
	// Set up authentication information.
	auth := smtp.PlainAuth("", "michael@cloud3000.com",
		"********",
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

func listenMR(conntype string, connhost string, connport string) {

	// Listen for incoming connections.
	l, err := net.Listen(conntype, connhost+":"+connport)
	if err != nil {
		fmt.Println("Error listening:", err.Error())
		os.Exit(1)
	}
	// Close the listener when the application closes.
	defer l.Close()
	fmt.Println("Listening on " + connhost + ":" + connport)
	syslog.Syslogf(syslog.LOG_INFO, "Listening on: %s:%s ", connhost, connport)
	for {
		// Listen for an incoming connection.
		conn, err := l.Accept()
		if err != nil {
			fmt.Println("Error accepting: ", err.Error())
			os.Exit(1)
		}
		// Handle connections in a new goroutine.
		go runMR(conn)
	}
}

func runMR(conn net.Conn) { // This a go routine thread ea. 'go runMR'

	// here we are preparing to pass the socket FD to the child process
	conn2, _ := conn.(*net.TCPConn).File()
	defer conn2.Close()
	d := conn2.Fd()
	//
	// We'll give the child process 3 seconds (4ever)
	// to grap the socket, b4 we close our end of it.
	time.AfterFunc(3*time.Second, func() {
		fmt.Printf("close conn: %s\n", conn.RemoteAddr())
		conn.Close()
	})

	init := "./bin/XML_MR_Receipt" // Child process
	// the FD on the cmdline, does not work.
	initArgs := []string{strconv.Itoa(int(d))}
	// For some reason the child always gets the socket in FD 3

	cmd := exec.Command(init, initArgs...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.ExtraFiles = []*os.File{conn2}

	if err := cmd.Start(); err != nil {
		fmt.Printf("Start %s error: %v\n", init, err)
		efrom := "customer@cloud3000.com"
		eto := "edimgr@cloud3000.com"
		esub := "[EDI] private_input ERROR, starting child process."
		emsg := fmt.Sprintf(
			"Child Process: %s\n"+
				"        Error: %s\n"+
				"    Date Time: %s\n",
			init,
			err.Error(),
			time.Now().Format("2006-01-02, 15:04:05"))
		ediEmail(efrom, eto, esub, emsg)
		os.Exit(1)
	}

	if err := cmd.Wait(); err != nil {
		fmt.Printf("Wait %s error: %v\n", init, err)
		efrom := "customer@cloud3000.com"
		eto := "edimgr@cloud3000.com"
		esub := "[EDI] private_input ERROR, death of child process."
		emsg := fmt.Sprintf(
			"Child Process: %s\n"+
				"        Error: %s\n"+
				"    Date Time: %s\n",
			init,
			fmt.Sprintf("Returned a bad exit status, %s", err.Error()),
			time.Now().Format("2006-01-02, 15:04:05"))
		ediEmail(efrom, eto, esub, emsg)
		os.Exit(1)
	}
}

// Handles incoming main requests.
func handleRequest(conn net.Conn) {
	// ToDo.. someday this may be used as a command interface to this process
	// commands: status, reset, stop (waits for children), abort (like kill -9)
	fmt.Printf("Connect from %v to NOP handler", conn.LocalAddr())
	conn.Close()
}

func main() {
	go listenMR(SERVER_TYPE, SERVER_HOST, MR_PORT)
	// Listen for incoming connections.
	l, err := net.Listen(SERVER_TYPE, SERVER_HOST+":"+CMD_PORT)
	if err != nil {
		fmt.Println("net.Listen Port %s Error:", err.Error())
		os.Exit(1)
	}
	// Close the listener when the application closes.
	defer l.Close()

	fmt.Println("Listening on " + SERVER_HOST + ":" + CMD_PORT)
	syslog.Syslogf(syslog.LOG_INFO, "Listening on: %s:%s ", SERVER_HOST, CMD_PORT)
	for {
		// Listen for an incoming connection.
		conn, err := l.Accept()
		if err != nil {
			fmt.Println("Error accepting: ", err.Error())
			break
		}
		// Handle connections in a new goroutine.
		go handleRequest(conn)
	}
}
