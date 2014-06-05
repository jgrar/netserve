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

	ERROR = log.New(os.Stderr, PROGRAM_NAME + ": ERROR: ", log.LstdFlags)
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

	send := make(chan []byte, 1)
	shutdown := make(chan int, 1)
	
	clients := make(Clients, 1)
	remove := make(chan *Client, 1)

	clients <- nil

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
	go c.Run(send, remove)

	go func (con net.Conn, send chan []byte, clients chan []*Client, shutdown chan int) {

		senderQuit := make(chan int, 1)
		recverQuit := make(chan int, 1)

		go func () {
			for {
				select {
					default:
						msg := make([]byte, 1024)

						n, err := con.Read(msg)
						if err != nil {
							if err != io.EOF {
								ERROR.Println(err)
							}
							recverQuit <-1
							return
						}

						m := msg[:n]
						log.Printf(">> %v\n", string(m))

						c := <-clients
						for i := range c {
							c[i].recv <-m
						}
						clients <-c

					case <-senderQuit:
						shutdown <-1
						return
				}
			}
		}()

		for {
			select {
				case msg := <-send:
					_, err := con.Write(msg)

					if err != nil {
						ERROR.Println(err)
						senderQuit <-1
						return
					}
				case <-recverQuit:
					shutdown <-1
					return
			}
		}

	}(remote, send, clients, shutdown)

	go func (clients Clients, shutdown chan int) {
		for {
			con, err := local.Accept()

			if err != nil {
				shutdown <-1
				return
			}

			client := NewClient(con)
			clients.Add(client)
			go client.Run(send, remove)
		}
	}(clients, shutdown)

LOOP:
	for {
		select {
			case client := <-remove:
				clients.Remove(client)

			case <-shutdown:
				break LOOP
		}
	}

	local.Close()
}

type Client struct{
	con net.Conn
	recv chan []byte
}

func NewClient (con net.Conn) *Client {
	return &Client{con, make(chan []byte)}
}

func (c *Client) Run (send chan []byte, remove chan *Client) {

	senderQuit := make(chan int, 1)
	recverQuit := make(chan int, 1)

	go func () {
		for {
			select {
				default:
					msg := make([]byte, 1024)

					n, err := c.con.Read(msg)

					if err != nil {
						if err != io.EOF {
							ERROR.Println(err)
						}
						senderQuit <-1
						return
					}
					m := msg[:n]

					log.Printf("<< %v\n", string(m))
					send <-m

				case <-recverQuit:
					remove <-c
					return
			}
		}
	}()

	for {
		select {
			case msg := <-c.recv:
				_, err := c.con.Write(msg)

				if err != nil {
					log.Println(err)
					recverQuit <-1
					return
				}
			case <-senderQuit:
				remove <-c
				return
		}
	}
}

type Clients chan []*Client

func (cl Clients) Add (client *Client) {
	cl <- append(<-cl, client)
}

func (cl Clients) Remove (client *Client) {
	c := <-cl
	z := len(c) - 1
	for i := range c {
		if c[i] == client {
			close(c[i].recv)
			c[i] = c[z]
			c[z] = nil
			c = c[:z]
			break
		}
	}
	cl <-c
}

