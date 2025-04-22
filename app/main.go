package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"strings"
)

// Ensures gofmt doesn't remove the "net" and "os" imports above (feel free to remove this!)
var _ = net.Listen
var _ = os.Exit

func echo(path string) string {
	body := strings.Split(path, "/echo/")[1]
	response := "HTTP/1.1 200 OK\r\n"
	content_type := "text/plain"
	content_length := len(body)
	response += fmt.Sprintf("Content-Type: %s\r\nContent-Length: %d\r\n\r\n%s", content_type, content_length, body)
	return response
}

func userAgent(path string) string {
	body := strings.Split(strings.Split(path, "User-Agent: ")[1], "\r\n")[0]
	response := "HTTP/1.1 200 OK\r\n"
	content_type := "text/plain"
	content_length := len(body)
	response += fmt.Sprintf("Content-Type: %s\r\nContent-Length: %d\r\n\r\n%s", content_type, content_length, body)
	return response
}

func files(path string, servePath string) string {
	file := strings.Split(path, "/files/")[1]
	content_type := "application/octet-stream"
	filePath := fmt.Sprintf("%s%s", servePath, file)
	response := "HTTP/1.1 200 OK\r\n"

	_, err := os.Stat(filePath)
	if os.IsNotExist(err) {
		return "HTTP/1.1 404 Not Found\r\n\r\n"
	}

	fileContent, err := os.ReadFile(filePath)
	if err != nil {
		log.Printf("Error reading file: %v", err)
		return "HTTP/1.1 404 Not Found\r\n\r\n"
	}

	content_length := len(fileContent)
	response += fmt.Sprintf("Content-Type: %s\r\nContent-Length: %d\r\n\r\n%s", content_type, content_length, fileContent)
	return response
}

func do(conn net.Conn, servePath string) {
	buff := make([]byte, 1024)
	_, err := conn.Read(buff)
	if err != nil {
		log.Fatal(err)
	}

	request := string(buff)
	parts := strings.Split(request, "\r\n")

	response := ""
	for i := 0; i < len(parts); i++ {
		if strings.Contains(parts[i], "GET") {
			log.Printf("Request: %s", parts[i])
			path := strings.Split(parts[i], " ")[1]
			if path == "/" {
				response = "HTTP/1.1 200 OK\r\n\r\n"
			} else if strings.Contains(path, "/files") {
				response = files(path, servePath)
			} else if strings.Contains(path, "/echo") {
				response = echo(path)
			} else if strings.Contains(path, "/user-agent") {
				response = userAgent(request)
			} else {
				response = "HTTP/1.1 404 Not Found\r\n\r\n"
			}
		}
	}

	conn.Write([]byte(response))
	conn.Close()
}

func main() {
	// You can use print statements as follows for debugging, they'll be visible when running tests.
	directory := flag.String("directory", ".", "Directory to serve files from")
	flag.Parse()
	servePath := *directory

	_, err := os.Stat(servePath)
	if os.IsNotExist(err) {
		fmt.Printf("Directory %s does not exist\n", servePath)
		os.Exit(1)
	}

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

		go do(conn, servePath)
	}
}
