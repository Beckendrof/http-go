package main

import (
	"fmt"
	"log"
	"net"
	"os"
	"strings"
)

// Ensures gofmt doesn't remove the "net" and "os" imports above (feel free to remove this!)
var _ = net.Listen
var _ = os.Exit

func do(conn net.Conn) {
	buff := make([]byte, 1024)
	_, err := conn.Read(buff)
	if err != nil {
		log.Fatal(err)
	}
	// Parse the HTTP request
	request := string(buff)
	parts := strings.Split(request, "\r\n")

	response := "\r\nContent-Type: text/plain\r\nContent-Length: "
	path := strings.Split(parts[0], " ")[1]
	if strings.Contains(path, "/echo") || path == "/" {
		if path == "/" || path == "/echo" {
			response = "HTTP/1.1 200 OK" + response + "0\r\n\r\n"
		} else {
			param := strings.Split(path, "/")[2]
			response = "HTTP/1.1 200 OK" + response + fmt.Sprint(len(param)) + "\r\n\r\n" + param
		}
	} else {
		response = "HTTP/1.1 404 Not Found\r\n\r\n"
	}

	conn.Write([]byte(response))

	conn.Close()
}

func main() {
	// You can use print statements as follows for debugging, they'll be visible when running tests.
	fmt.Println("Logs from your program will appear here!")

	l, err := net.Listen("tcp", "0.0.0.0:4221")
	if err != nil {
		fmt.Println("Failed to bind to port 4221")
		os.Exit(1)
	}
	for {
		conn, err := l.Accept()
		if err != nil {
			fmt.Println("Error accepting connection: ", err.Error())
			os.Exit(1)
		}
		go do(conn)
	}
}
