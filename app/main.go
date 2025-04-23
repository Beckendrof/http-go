package main

import (
	"bufio" // Import bufio for buffered reading
	"bytes"
	"compress/gzip"
	"flag"
	"fmt"
	"io" // Import io for EOF
	"log"
	"net"
	"os"
	"strings"
	"time" // Import time for deadlines
)

// Global variable to hold the directory path
var serveDirectory string

// echo handles the /echo/ path, including potential gzip compression.
// It now takes an additional argument to signal if the connection should close.
func echo(path string, requestHeaders map[string]string, closeConnection bool) string { // Pass parsed headers and close flag
	// Extract the message part from the path
	if !strings.HasPrefix(path, "/echo/") {
		// Note: Sending Connection: close on error responses is also valid
		return "HTTP/1.1 400 Bad Request\r\nConnection: close\r\n\r\n"
	}
	body := strings.SplitN(path, "/echo/", 2)[1]

	response := "HTTP/1.1 200 OK\r\n"
	contentType := "text/plain"

	// --- Gzip Compression Check ---
	acceptEncodingHeader := requestHeaders["accept-encoding"] // Use parsed header
	canGzip := false
	if acceptEncodingHeader != "" {
		encodings := strings.Split(acceptEncodingHeader, ",")
		for _, enc := range encodings {
			if strings.TrimSpace(enc) == "gzip" {
				canGzip = true
				break
			}
		}
	}
	// --- End Gzip Check ---
	var buffer bytes.Buffer
	if canGzip {
		gzipWriter := gzip.NewWriter(&buffer)
		_, writeErr := gzipWriter.Write([]byte(body))
		closeErr := gzipWriter.Close()

		if writeErr != nil || closeErr != nil {
			log.Printf("Gzip compression failed: writeErr=%v, closeErr=%v", writeErr, closeErr)
			return "HTTP/1.1 500 Internal Server Error\r\nConnection: close\r\n\r\n"
		}

		compressedBodyBytes := buffer.Bytes()
		contentLength := len(compressedBodyBytes)

		response += "Content-Encoding: gzip\r\n"
		response += fmt.Sprintf("Content-Type: %s\r\n", contentType)
		response += fmt.Sprintf("Content-Length: %d\r\n", contentLength)

	} else {
		contentLength := len(body)
		response += fmt.Sprintf("Content-Type: %s\r\n", contentType)
		response += fmt.Sprintf("Content-Length: %d\r\n", contentLength)
		// Append body later after potentially adding Connection header
	}

	// Add Connection: close header if needed
	if closeConnection {
		response += "Connection: close\r\n"
	}

	// Add final CRLF before body
	response += "\r\n"

	// Add body (append separately if not gzipped to handle potential Connection header)
	if canGzip {
		response += buffer.String() // Append compressed bytes as string
	} else {
		response += body // Append plain body
	}

	return response
}

// userAgent extracts the User-Agent header value from parsed headers.
// It now takes an additional argument to signal if the connection should close.
func userAgent(requestHeaders map[string]string, closeConnection bool) string { // Pass parsed headers and close flag
	userAgent := requestHeaders["user-agent"] // Use parsed header

	response := "HTTP/1.1 200 OK\r\n"
	contentType := "text/plain"
	contentLength := len(userAgent)
	response += fmt.Sprintf("Content-Type: %s\r\n", contentType)
	response += fmt.Sprintf("Content-Length: %d\r\n", contentLength)

	// Add Connection: close header if needed
	if closeConnection {
		response += "Connection: close\r\n"
	}

	response += "\r\n" // End of headers
	response += userAgent
	return response
}

// getFiles handles GET requests for files.
// It now takes an additional argument to signal if the connection should close.
func getFiles(path string, closeConnection bool) string { // Pass close flag
	if !strings.HasPrefix(path, "/files/") {
		return "HTTP/1.1 400 Bad Request\r\nConnection: close\r\n\r\n"
	}
	fileName := strings.SplitN(path, "/files/", 2)[1]
	if fileName == "" {
		return "HTTP/1.1 404 Not Found\r\nConnection: close\r\n\r\n"
	}

	// NOTE: Still vulnerable to path traversal!
	filePath := serveDirectory + string(os.PathSeparator) + fileName

	fileContent, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return "HTTP/1.1 404 Not Found\r\nConnection: close\r\n\r\n"
		}
		log.Printf("Error reading file %s: %v", filePath, err)
		return "HTTP/1.1 500 Internal Server Error\r\nConnection: close\r\n\r\n"
	}

	response := "HTTP/1.1 200 OK\r\n"
	contentType := "application/octet-stream"
	contentLength := len(fileContent)
	response += fmt.Sprintf("Content-Type: %s\r\n", contentType)
	response += fmt.Sprintf("Content-Length: %d\r\n", contentLength)

	// Add Connection: close header if needed
	if closeConnection {
		response += "Connection: close\r\n"
	}

	response += "\r\n" // End of headers
	response += string(fileContent)
	return response
}

// postFiles handles POST requests for files.
// It now takes an additional argument to signal if the connection should close.
// NOTE: This simplistic body reading is insufficient for Keep-Alive.
func postFiles(path string, reader *bufio.Reader, headers map[string]string, closeConnection bool) string { // Pass close flag
	if !strings.HasPrefix(path, "/files/") {
		return "HTTP/1.1 400 Bad Request\r\nConnection: close\r\n\r\n"
	}
	fileName := strings.SplitN(path, "/files/", 2)[1]
	if fileName == "" {
		return "HTTP/1.1 400 Bad Request\r\nConnection: close\r\n\r\n"
	}

	// NOTE: Still vulnerable to path traversal!
	filePath := serveDirectory + string(os.PathSeparator) + fileName

	// --- Incredibly Basic Body Reading - Highly Flawed ---
	var bodyBuffer bytes.Buffer
	tempBuff := make([]byte, 1024)
	bytesReadTotal := 0
	contentLengthStr := headers["content-length"]
	contentLength := 0
	fmt.Sscan(contentLengthStr, &contentLength) // Basic conversion

	for bytesReadTotal < contentLength {
		nToRead := len(tempBuff)
		if contentLength > 0 && contentLength-bytesReadTotal < nToRead { // Check contentLength > 0
			nToRead = contentLength - bytesReadTotal
		} else if contentLength == 0 { // If no Content-Length, read only once (very basic)
			nToRead = len(tempBuff)
		}

		n, err := reader.Read(tempBuff[:nToRead])
		if n > 0 {
			bodyBuffer.Write(tempBuff[:n])
			bytesReadTotal += n
		}
		// If no Content-Length, break after first read attempt
		if contentLength == 0 {
			break
		}
		if err != nil {
			if err != io.EOF {
				log.Printf("Error reading POST body for %s: %v", filePath, err)
			}
			break
		}
		if n == 0 {
			break
		}
	}
	// --- End Flawed Body Reading ---

	err := os.WriteFile(filePath, bodyBuffer.Bytes(), 0644)
	if err != nil {
		log.Printf("Error writing file %s: %v", filePath, err)
		return "HTTP/1.1 500 Internal Server Error\r\nConnection: close\r\n\r\n"
	}

	response := "HTTP/1.1 201 Created\r\n"
	// Add Connection: close header if needed
	if closeConnection {
		response += "Connection: close\r\n"
	}
	response += "\r\n" // End of headers (no body for 201)
	return response
}

// do handles a single connection, looping to process multiple requests.
func do(conn net.Conn) {
	defer conn.Close()
	reader := bufio.NewReader(conn)

	for {
		err := conn.SetReadDeadline(time.Now().Add(10 * time.Second))
		if err != nil {
			log.Printf("Error setting read deadline: %v", err)
			return
		}

		requestLine, err := reader.ReadString('\n')
		if err != nil {
			if err != io.EOF {
				log.Printf("Error reading request line: %v", err)
			}
			return
		}
		requestLine = strings.TrimSpace(requestLine)
		if requestLine == "" {
			continue
		}

		requestParts := strings.Split(requestLine, " ")
		if len(requestParts) < 3 {
			log.Printf("Malformed request line: %q", requestLine)
			conn.Write([]byte("HTTP/1.1 400 Bad Request\r\nConnection: close\r\n\r\n")) // Add header here too
			return
		}
		method := requestParts[0]
		path := requestParts[1]

		log.Printf("Request: %s %s", method, path)

		headers := make(map[string]string)
		for {
			headerLine, err := reader.ReadString('\n')
			if err != nil {
				log.Printf("Error reading headers: %v", err)
				return
			}
			headerLine = strings.TrimSpace(headerLine)
			if headerLine == "" {
				break
			}
			parts := strings.SplitN(headerLine, ":", 2)
			if len(parts) == 2 {
				key := strings.ToLower(strings.TrimSpace(parts[0]))
				value := strings.TrimSpace(parts[1])
				headers[key] = value
			}
		}

		// --- Determine if connection should close AFTER this response ---
		// Default for HTTP/1.1 is keep-alive unless specified otherwise
		closeConnection := false
		if strings.ToLower(headers["connection"]) == "close" {
			closeConnection = true
			log.Println("Client requested connection close.")
		}
		// Could add logic here for HTTP/1.0 checks if needed

		// --- Routing Logic ---
		response := "" // Initialize response string
		switch method {
		case "GET":
			if path == "/" {
				response = "HTTP/1.1 200 OK\r\n"
				if closeConnection {
					response += "Connection: close\r\n"
				}
				response += "\r\n"
			} else if strings.HasPrefix(path, "/echo/") {
				response = echo(path, headers, closeConnection) // Pass close flag
			} else if path == "/user-agent" {
				response = userAgent(headers, closeConnection) // Pass close flag
			} else if strings.HasPrefix(path, "/files/") {
				response = getFiles(path, closeConnection) // Pass close flag
			} else {
				response = "HTTP/1.1 404 Not Found\r\n"
				if closeConnection {
					response += "Connection: close\r\n"
				}
				response += "\r\n"
			}
		case "POST":
			if strings.HasPrefix(path, "/files/") {
				response = postFiles(path, reader, headers, closeConnection) // Pass close flag
			} else {
				response = "HTTP/1.1 404 Not Found\r\n"
				if closeConnection {
					response += "Connection: close\r\n"
				}
				response += "\r\n"
			}
		default:
			response = "HTTP/1.1 405 Method Not Allowed\r\n"
			if closeConnection {
				response += "Connection: close\r\n"
			}
			response += "\r\n"
		}
		// --- End Routing ---

		// --- Write Response ---
		err = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
		if err != nil {
			log.Printf("Error setting write deadline: %v", err)
			return
		}
		_, writeErr := conn.Write([]byte(response))
		if writeErr != nil {
			log.Printf("Error writing response: %v", writeErr)
			return
		}

		// --- If connection should close, exit the loop ---
		if closeConnection {
			return // Exit loop, defer conn.Close() will execute
		}

		// Loop continues here for the next request...
	}
}

func main() {
	// Define and parse the --directory flag
	dirFlag := flag.String("directory", ".", "Directory to serve files from")
	flag.Parse()
	serveDirectory = *dirFlag // Assign to the global variable

	// Check if the directory exists
	info, err := os.Stat(serveDirectory)
	if err != nil {
		if os.IsNotExist(err) {
			log.Fatalf("Error: Directory '%s' does not exist", serveDirectory)
		}
		log.Fatalf("Error stating directory '%s': %v", serveDirectory, err)
	}
	if !info.IsDir() {
		log.Fatalf("Error: '%s' is not a directory", serveDirectory)
	}

	// Start listening on the specified port
	addr := "0.0.0.0:4221"
	l, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatalf("Failed to bind to port %s: %v", addr, err)
	}
	defer l.Close()
	log.Printf("Server listening on %s...", addr)
	log.Printf("Serving files from directory: %s", serveDirectory)

	// Accept connections in a loop
	for {
		conn, err := l.Accept()
		if err != nil {
			log.Printf("Error accepting connection: %v", err)
			continue
		}
		// Handle each connection concurrently in a goroutine
		go do(conn)
	}
}
