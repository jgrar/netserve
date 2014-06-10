package main

import (
	"net"
	"crypto/tls"
	"time"

	"os"
	"log"
	"flag"
	"io"
	"io/ioutil"
)

const PROGRAM_NAME = "netserve"

var (
	verbose = flag.Bool("v", false, "print additional program information")

	ssl = flag.Bool("ssl", false, "use ssl for remote connection")
	path = flag.String("path", PROGRAM_NAME + ".sock", "unix socket path name")
	host string
	port string

	sslConfig = tls.Config{
		InsecureSkipVerify: true,
	}

	ERROR = log.New(os.Stderr, PROGRAM_NAME + ": ERROR: ", log.Lshortfile)
)

func init () {
	flag.Usage = func () {
		ERROR.Println("usage: " + PROGRAM_NAME + " [options] host port")
		flag.PrintDefaults()
	}
}

func main () {
	flag.Parse()
	
	if !*verbose {
		log.SetOutput(ioutil.Discard)
	}

	if flag.NArg() < 2 {
		ERROR.Println("missing arguments")
		flag.Usage()
		os.Exit(1)
	}

	host, port = flag.Arg(0), flag.Arg(1)

	shutdown := make(chan int, 1)
	clients := NewClients()

	local, err := net.Listen("unix", *path)
	if err != nil {
		ERROR.Fatal(err)
	}

	con, err := local.Accept()

	if err != nil {
		local.Close()
		ERROR.Fatalf("error waiting for initial client on %q: %s", *path, err)
	}

	remote, err := net.DialTimeout("tcp", host + ":" + port, 30 * time.Second)
	if err != nil {
		local.Close()
		ERROR.Fatalf("could not connect to \"%s:%s\": %s\n", host, port, err)
	}

	if *ssl {
		sslConfig.ServerName = host
		remote = tls.Client(remote, &sslConfig)
	}

	c := NewClient(con)
	clients.Add(c)
	go c.Run(remote, shutdown, clients)

	go func () {

		for {
			msg := make([]byte, 1024)

			n, err := remote.Read(msg)
			if err != nil {
				if err != io.EOF {
					ERROR.Println(err)
				}
				shutdown <-1
				return
			}

			msg = msg[:n]

			log.Printf(">> %v\n", string(msg))

			c := <-clients
			for i := range c {
				_, err := c[i].con.Write(msg)

				if err != nil {
					if err != io.EOF {
						ERROR.Println(err)
					}
					c[i].remove <-1
				}
			}
			clients <-c
		}
	}()

	go func () {
		for {
			con, err := local.Accept()

			if err != nil {
				ERROR.Println(err)
				shutdown <-1
				return
			}

			client := NewClient(con)
			clients.Add(client)
			go client.Run(remote, shutdown, clients)
		}
	}()

	<-shutdown
	remote.Close()
	local.Close()
}

type Client struct{
	con net.Conn
	remove chan int
}

func NewClient (con net.Conn) *Client {
	return &Client{con, make(chan int, 1)}
}

func (c *Client) Run (remote net.Conn, shutdown chan int, clients Clients) {

	for {
		select {

		default:
			msg := make([]byte, 1024)
			
			n, err := c.con.Read(msg)
			if err != nil {
				if err != io.EOF {
					ERROR.Println(err)
				}
				c.remove <-1
				continue
			}
			msg = msg[:n]

			log.Printf("<< %v\n", string(msg))

			n, err = remote.Write(msg)
			if err != nil {
				if err != io.EOF {
					ERROR.Println(err)
				}
				shutdown <-1
			}
		case <-c.remove:
			clients.Remove(c)
			return
		}

	}
}

type Clients chan []*Client

func NewClients () Clients {
	clients := make(Clients, 1)
	clients <-nil
	return clients
}

func (cl Clients) Add (client *Client) {
	clients <- append(<-clients, client)
}

func (clients Clients) Remove (client *Client) {
	a := <-clients
	z := len(a) - 1
	for i := range a {
		if a[i] == client {
			close(a[i].remove)
			a[i].con.Close()

			a[i] = a[z]
			a[z] = nil
			a = a[:z]
			z--
		}
	}
	clients <-a
}
