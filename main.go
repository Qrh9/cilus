package main

import (
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/exec"
	"sync"

	"github.com/creack/pty"
)

const socketPath = "/tmp/clius.sock"

func server() error {
	if _, err := os.Stat(socketPath); err == nil {
		os.Remove(socketPath)
	}

	ln, err := net.Listen("unix", socketPath)
	if err != nil {
		return fmt.Errorf("failed to listen on unix socket: %w", err)
	}
	defer ln.Close()

	cmd := exec.Command("bash")
	ptmx, err := pty.Start(cmd)
	if err != nil {
		return fmt.Errorf("failed to start pty: %w", err)
	}
	defer func() {
		ptmx.Close()
		cmd.Wait()
	}()

	var clientsMu sync.Mutex
	clients := make(map[net.Conn]bool)

	broadcast := func(data []byte) {
		clientsMu.Lock()
		defer clientsMu.Unlock()
		for c := range clients {
			_, err := c.Write(data)
			if err != nil {
				c.Close()
				delete(clients, c)
			}
		}
	}

	go func() {
		buf := make([]byte, 1024)
		for {
			n, err := ptmx.Read(buf)
			if err != nil {
				log.Printf("Error reading from pty: %v", err)
				break
			}
			if n > 0 {
				broadcast(buf[:n])
			}
		}
	}()

	log.Printf("Clius session active. Waiting for clients on %s", socketPath)
	for {
		conn, err := ln.Accept()
		if err != nil {
			log.Printf("Error accepting connection: %v", err)
			continue
		}
		clientsMu.Lock()
		clients[conn] = true
		clientsMu.Unlock()
		log.Printf("New client connected")
		conn.Write([]byte("Welcome to clius shared session!\n"))
		go func(c net.Conn) {
			defer func() {
				clientsMu.Lock()
				delete(clients, c)
				clientsMu.Unlock()
				c.Close()
			}()
			io.Copy(ptmx, c)
		}(conn)
	}
}

func client() error {
	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		return fmt.Errorf("failed to connect to clius session: %w", err)
	}
	defer conn.Close()

	go io.Copy(os.Stdout, conn)
	_, err = io.Copy(conn, os.Stdin)
	if err != nil {
		return fmt.Errorf("error copying input: %w", err)
	}
	return nil
}

func main() {
	if _, err := os.Stat(socketPath); os.IsNotExist(err) {
		log.Println("opening server")
		go func() {
			if err := client(); err != nil {
				log.Fatalf("Client error: %v", err)
			}
		}()
		if err := server(); err != nil {
			log.Fatalf("Server error: %v", err)
		}
	} else {
		if err := client(); err != nil {
			log.Fatalf("Client error: %v", err)
		}
	}
}
