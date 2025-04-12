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

	response := ""
	body := ""
	content_type := "text/plain"
	for i := 0; i < len(parts); i++ {
		if strings.Contains(parts[i], "GET") {
			path := strings.Split(parts[i], " ")[1]
			if path == "/" {
				response += "HTTP/1.1 200 OK\r\n"
			} else if strings.Contains(path, "/echo") {
				body = strings.Split(path, "/echo/")[1]
				response += "HTTP/1.1 200 OK\r\n"
			} else if strings.Contains(path, "/user-agent") {
				body = strings.Split(strings.Split(request, "User-Agent: ")[1], "\r\n")[0]
				response += "HTTP/1.1 200 OK\r\n"
			} else {
				response += "HTTP/1.1 404 Not Found\r\n"
			}
		} else if strings.Contains(parts[i], "Content-Type: ") {
			content_type = strings.Split(parts[i], ": ")[1]
		}
	}
	content_length := len(body)
	response += fmt.Sprintf("Content-Type: %s\r\nContent-Length: %d\r\n\r\n%s", content_type, content_length, body)

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
