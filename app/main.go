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
func echo(path string, requestHeaders map[string]string) string { // Pass parsed headers
	// Extract the message part from the path
	if !strings.HasPrefix(path, "/echo/") {
		return "HTTP/1.1 400 Bad Request\r\n\r\n"
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

	if canGzip {
		var buffer bytes.Buffer
		gzipWriter := gzip.NewWriter(&buffer)
		_, writeErr := gzipWriter.Write([]byte(body))
		closeErr := gzipWriter.Close()

		if writeErr != nil || closeErr != nil {
			log.Printf("Gzip compression failed: writeErr=%v, closeErr=%v", writeErr, closeErr)
			return "HTTP/1.1 500 Internal Server Error\r\n\r\n"
		}

		compressedBodyBytes := buffer.Bytes()
		contentLength := len(compressedBodyBytes)

		response += fmt.Sprintf("Content-Encoding: gzip\r\n")
		response += fmt.Sprintf("Content-Type: %s\r\n", contentType)
		response += fmt.Sprintf("Content-Length: %d\r\n", contentLength)
		response += "\r\n"
		response += string(compressedBodyBytes)

	} else {
		contentLength := len(body)
		response += fmt.Sprintf("Content-Type: %s\r\n", contentType)
		response += fmt.Sprintf("Content-Length: %d\r\n", contentLength)
		response += "\r\n"
		response += body
	}

	return response
}

// userAgent extracts the User-Agent header value from parsed headers.
func userAgent(requestHeaders map[string]string) string { // Pass parsed headers
	userAgent := requestHeaders["user-agent"] // Use parsed header

	response := "HTTP/1.1 200 OK\r\n"
	contentType := "text/plain"
	contentLength := len(userAgent)
	response += fmt.Sprintf("Content-Type: %s\r\n", contentType)
	response += fmt.Sprintf("Content-Length: %d\r\n", contentLength)
	response += "\r\n"
	response += userAgent
	return response
}

// getFiles handles GET requests for files.
func getFiles(path string) string {
	if !strings.HasPrefix(path, "/files/") {
		return "HTTP/1.1 400 Bad Request\r\n\r\n"
	}
	fileName := strings.SplitN(path, "/files/", 2)[1]
	if fileName == "" {
		return "HTTP/1.1 404 Not Found\r\n\r\n"
	}

	// NOTE: Still vulnerable to path traversal!
	filePath := serveDirectory + string(os.PathSeparator) + fileName

	fileContent, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return "HTTP/1.1 404 Not Found\r\n\r\n"
		}
		log.Printf("Error reading file %s: %v", filePath, err)
		return "HTTP/1.1 500 Internal Server Error\r\n\r\n"
	}

	response := "HTTP/1.1 200 OK\r\n"
	contentType := "application/octet-stream"
	contentLength := len(fileContent)
	response += fmt.Sprintf("Content-Type: %s\r\n", contentType)
	response += fmt.Sprintf("Content-Length: %d\r\n", contentLength)
	response += "\r\n"
	response += string(fileContent)
	return response
}

// postFiles handles POST requests for files.
// NOTE: This simplistic body reading is insufficient for Keep-Alive.
// A real implementation needs to read exactly Content-Length bytes
// or handle chunked encoding after the headers.
func postFiles(path string, reader *bufio.Reader, headers map[string]string) string {
	if !strings.HasPrefix(path, "/files/") {
		return "HTTP/1.1 400 Bad Request\r\n\r\n"
	}
	fileName := strings.SplitN(path, "/files/", 2)[1]
	if fileName == "" {
		return "HTTP/1.1 400 Bad Request\r\n\r\n"
	}

	// NOTE: Still vulnerable to path traversal!
	filePath := serveDirectory + string(os.PathSeparator) + fileName

	// --- Incredibly Basic Body Reading - Highly Flawed ---
	// This assumes the body immediately follows headers and reads *some* data.
	// It DOES NOT correctly handle Content-Length or Transfer-Encoding.
	// It will likely read too much or too little, breaking subsequent requests.
	// For demonstration purposes only. A proper implementation is much harder.
	var bodyBuffer bytes.Buffer
	tempBuff := make([]byte, 1024) // Read in chunks
	bytesReadTotal := 0
	contentLengthStr := headers["content-length"]
	contentLength := 0
	fmt.Sscan(contentLengthStr, &contentLength) // Basic conversion

	// Attempt to read roughly Content-Length bytes (still flawed)
	for bytesReadTotal < contentLength {
		nToRead := len(tempBuff)
		if contentLength-bytesReadTotal < nToRead {
			nToRead = contentLength - bytesReadTotal
		}
		n, err := reader.Read(tempBuff[:nToRead])
		if n > 0 {
			bodyBuffer.Write(tempBuff[:n])
			bytesReadTotal += n
		}
		if err != nil {
			if err != io.EOF {
				log.Printf("Error reading POST body for %s: %v", filePath, err)
			}
			break // Stop reading on error or EOF
		}
		if n == 0 { // Should not happen often with Read unless EOF
			break
		}
	}
	// --- End Flawed Body Reading ---

	err := os.WriteFile(filePath, bodyBuffer.Bytes(), 0644)
	if err != nil {
		log.Printf("Error writing file %s: %v", filePath, err)
		return "HTTP/1.1 500 Internal Server Error\r\n\r\n"
	}

	return "HTTP/1.1 201 Created\r\n\r\n"
}

// do handles a single connection, looping to process multiple requests.
func do(conn net.Conn) {
	// Close the connection when this function eventually exits
	// (e.g., due to error, timeout, or client closing)
	defer conn.Close()

	// Use a buffered reader for more efficient reading
	reader := bufio.NewReader(conn)

	// Loop to handle multiple requests on the same connection
	for {
		// Set a deadline for reading the next request
		// Adjust the duration as needed (e.g., 5-10 seconds)
		err := conn.SetReadDeadline(time.Now().Add(10 * time.Second))
		if err != nil {
			log.Printf("Error setting read deadline: %v", err)
			return // Close connection if deadline can't be set
		}

		// --- Read Request Line ---
		requestLine, err := reader.ReadString('\n')
		if err != nil {
			// Handle errors: EOF means client closed connection cleanly.
			// Other errors (like timeout) should also close the connection.
			if err != io.EOF {
				log.Printf("Error reading request line: %v", err)
			}
			return // Exit loop and close connection
		}
		requestLine = strings.TrimSpace(requestLine) // Remove \r\n
		if requestLine == "" {
			// Sometimes keep-alive connections send empty lines; continue reading
			continue
		}

		// --- Parse Request Line ---
		requestParts := strings.Split(requestLine, " ")
		if len(requestParts) < 3 {
			log.Printf("Malformed request line: %q", requestLine)
			conn.Write([]byte("HTTP/1.1 400 Bad Request\r\n\r\n"))
			return // Close connection on malformed request
		}
		method := requestParts[0]
		path := requestParts[1]
		// protocol := requestParts[2] // Could check this (e.g., HTTP/1.0 needs Connection: keep-alive)

		log.Printf("Request: %s %s", method, path)

		// --- Read Headers ---
		headers := make(map[string]string)
		for {
			headerLine, err := reader.ReadString('\n')
			if err != nil {
				log.Printf("Error reading headers: %v", err)
				return // Close connection on header read error
			}
			headerLine = strings.TrimSpace(headerLine)
			if headerLine == "" {
				// Empty line signifies end of headers
				break
			}
			// Split header into key/value
			parts := strings.SplitN(headerLine, ":", 2)
			if len(parts) == 2 {
				key := strings.ToLower(strings.TrimSpace(parts[0]))
				value := strings.TrimSpace(parts[1])
				headers[key] = value
			} else {
				log.Printf("Malformed header line: %q", headerLine)
				// Optionally send 400 Bad Request and close
			}
		}

		// --- Routing Logic ---
		response := "HTTP/1.1 404 Not Found\r\n\r\n" // Default response
		switch method {
		case "GET":
			if path == "/" {
				response = "HTTP/1.1 200 OK\r\n\r\n"
			} else if strings.HasPrefix(path, "/echo/") {
				response = echo(path, headers) // Pass parsed headers
			} else if path == "/user-agent" {
				response = userAgent(headers) // Pass parsed headers
			} else if strings.HasPrefix(path, "/files/") {
				response = getFiles(path)
			}
		case "POST":
			if strings.HasPrefix(path, "/files/") {
				// Pass the reader and headers to handle body reading (still flawed)
				response = postFiles(path, reader, headers)
			}
		default:
			response = "HTTP/1.1 405 Method Not Allowed\r\n\r\n"
		}
		// --- End Routing ---

		// --- Write Response ---
		// Set a deadline for writing the response
		err = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
		if err != nil {
			log.Printf("Error setting write deadline: %v", err)
			return // Close connection
		}
		_, writeErr := conn.Write([]byte(response))
		if writeErr != nil {
			log.Printf("Error writing response: %v", writeErr)
			return // Close connection on write error
		}

		// --- Check for Connection Close Header ---
		// If client sent "Connection: close", break the loop to close
		if strings.ToLower(headers["connection"]) == "close" {
			log.Println("Client requested connection close.")
			return // Exit loop, defer conn.Close() will execute
		}
		// For HTTP/1.0, we'd need to check for "Connection: keep-alive" to *stay* open

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
