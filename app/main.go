package main

import (
	"bytes"
	"compress/gzip"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"strings"
)

// Global variable to hold the directory path
var serveDirectory string

// echo handles the /echo/ path, including potential gzip compression.
func echo(path string, request string) string {
	// Extract the message part from the path
	// Check if the path actually contains "/echo/" before splitting
	if !strings.Contains(path, "/echo/") {
		return "HTTP/1.1 400 Bad Request\r\n\r\n" // Or 404
	}
	body := strings.SplitN(path, "/echo/", 2)[1] // Use SplitN for safety

	// Default response headers
	response := "HTTP/1.1 200 OK\r\n"
	contentType := "text/plain"

	// --- Gzip Compression Check ---
	// Find the Accept-Encoding header line
	acceptEncodingHeader := ""
	requestLines := strings.Split(request, "\r\n")
	for _, line := range requestLines {
		// Case-insensitive check for the header name
		if strings.HasPrefix(strings.ToLower(line), "accept-encoding:") {
			acceptEncodingHeader = line
			break
		}
	}

	canGzip := false
	if acceptEncodingHeader != "" {
		// Extract the values part (e.g., "gzip, deflate")
		// Split after the first colon and trim whitespace
		encodingValues := strings.TrimSpace(strings.SplitN(acceptEncodingHeader, ":", 2)[1])
		// Check if "gzip" is present in the list of accepted encodings
		encodings := strings.Split(encodingValues, ",")
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
		// Write the original body bytes
		_, writeErr := gzipWriter.Write([]byte(body))
		closeErr := gzipWriter.Close() // Close is crucial

		if writeErr != nil || closeErr != nil {
			log.Printf("Gzip compression failed: writeErr=%v, closeErr=%v", writeErr, closeErr)
			// Fallback to non-gzipped response or send an error?
			// Sending 500 might be appropriate
			return "HTTP/1.1 500 Internal Server Error\r\n\r\n"
		}

		// *** IMPORTANT: Use the compressed bytes, NOT buffer.String() ***
		compressedBodyBytes := buffer.Bytes()
		contentLength := len(compressedBodyBytes)

		// Construct response with compressed body and headers
		response += "Content-Encoding: gzip\r\n"
		response += fmt.Sprintf("Content-Type: %s\r\n", contentType)
		response += fmt.Sprintf("Content-Length: %d\r\n", contentLength)
		response += "\r\n" // End of headers
		// Append the raw compressed bytes as a string (problematic but matches original request)
		response += string(compressedBodyBytes)

	} else {
		// Non-gzipped response
		contentLength := len(body)
		response += fmt.Sprintf("Content-Type: %s\r\n", contentType)
		response += fmt.Sprintf("Content-Length: %d\r\n", contentLength)
		response += "\r\n" // End of headers
		response += body
	}

	return response
}

// userAgent extracts the User-Agent header value from the raw request string.
func userAgent(request string) string {
	userAgent := ""
	lines := strings.Split(request, "\r\n")
	for _, line := range lines {
		// Case-insensitive check for the header name
		if strings.HasPrefix(strings.ToLower(line), "user-agent:") {
			// Extract value after the colon, trim leading/trailing space
			parts := strings.SplitN(line, ":", 2)
			if len(parts) == 2 {
				userAgent = strings.TrimSpace(parts[1])
			}
			break // Found the header
		}
	}

	// Construct the response
	response := "HTTP/1.1 200 OK\r\n"
	contentType := "text/plain"
	contentLength := len(userAgent)
	response += fmt.Sprintf("Content-Type: %s\r\n", contentType)
	response += fmt.Sprintf("Content-Length: %d\r\n", contentLength)
	response += "\r\n" // End of headers
	response += userAgent
	return response
}

// getFiles handles GET requests for files.
func getFiles(path string) string {
	// Extract filename, ensure path starts with /files/
	if !strings.HasPrefix(path, "/files/") {
		return "HTTP/1.1 400 Bad Request\r\n\r\n"
	}
	fileName := strings.SplitN(path, "/files/", 2)[1]
	if fileName == "" {
		return "HTTP/1.1 404 Not Found\r\n\r\n" // No filename provided
	}

	// Construct full path - NOTE: Still vulnerable to path traversal without path/filepath!
	// Be very careful running this with arbitrary directory flags.
	filePath := serveDirectory + string(os.PathSeparator) + fileName

	fileContent, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return "HTTP/1.1 404 Not Found\r\n\r\n"
		}
		log.Printf("Error reading file %s: %v", filePath, err)
		return "HTTP/1.1 500 Internal Server Error\r\n\r\n" // Other read error
	}

	// Construct response
	response := "HTTP/1.1 200 OK\r\n"
	contentType := "application/octet-stream"
	contentLength := len(fileContent)
	response += fmt.Sprintf("Content-Type: %s\r\n", contentType)
	response += fmt.Sprintf("Content-Length: %d\r\n", contentLength)
	response += "\r\n"              // End of headers
	response += string(fileContent) // Append file content as string
	return response
}

// postFiles handles POST requests for files.
func postFiles(path string, requestBody string) string {
	// Extract filename
	if !strings.HasPrefix(path, "/files/") {
		return "HTTP/1.1 400 Bad Request\r\n\r\n"
	}
	fileName := strings.SplitN(path, "/files/", 2)[1]
	if fileName == "" {
		return "HTTP/1.1 400 Bad Request\r\n\r\n" // No filename provided
	}

	// Construct full path - NOTE: Still vulnerable to path traversal!
	filePath := serveDirectory + string(os.PathSeparator) + fileName

	// Write the file content (requestBody)
	// Note: requestBody here is extracted very simply and might contain headers
	// if the parsing in 'do' isn't perfect. Also trims null bytes.
	trimmedBody := strings.TrimRight(requestBody, "\x00")
	err := os.WriteFile(filePath, []byte(trimmedBody), 0644)
	if err != nil {
		log.Printf("Error writing file %s: %v", filePath, err)
		return "HTTP/1.1 500 Internal Server Error\r\n\r\n"
	}

	// Respond with 201 Created
	return "HTTP/1.1 201 Created\r\n\r\n"
}

// do handles a single connection, reads one request, sends one response, and closes.
func do(conn net.Conn) {
	defer conn.Close() // Ensure connection is closed when function exits

	// Read the request data
	// Using a fixed buffer is fragile; requests > 1024 bytes will be truncated.
	buff := make([]byte, 1024)
	n, err := conn.Read(buff)
	if err != nil {
		// Log errors like client disconnecting prematurely, but don't crash server
		if err != io.EOF { // EOF is expected if client closes connection cleanly
			log.Printf("Error reading from connection: %v", err)
		}
		return // Stop processing for this connection
	}
	// Use only the bytes read
	request := string(buff[:n])

	// --- Basic HTTP Request Parsing ---
	lines := strings.SplitN(request, "\r\n", -1) // Split into lines
	if len(lines) == 0 {
		log.Println("Received empty request")
		return // Ignore empty request
	}

	requestLine := lines[0]
	requestParts := strings.Split(requestLine, " ")
	if len(requestParts) < 3 {
		log.Printf("Malformed request line: %s", requestLine)
		// Maybe send 400 Bad Request?
		conn.Write([]byte("HTTP/1.1 400 Bad Request\r\n\r\n"))
		return
	}

	method := requestParts[0]
	path := requestParts[1]
	// protocol := requestParts[2] // We assume HTTP/1.1 for response format

	log.Printf("Request: %s %s", method, path)

	response := "HTTP/1.1 404 Not Found\r\n\r\n" // Default response

	// --- Routing Logic ---
	switch method {
	case "GET":
		if path == "/" {
			response = "HTTP/1.1 200 OK\r\n\r\n"
		} else if strings.HasPrefix(path, "/echo/") {
			response = echo(path, request) // Pass full request for header parsing
		} else if path == "/user-agent" {
			response = userAgent(request) // Pass full request for header parsing
		} else if strings.HasPrefix(path, "/files/") {
			response = getFiles(path)
		}
		// else: response remains 404

	case "POST":
		if strings.HasPrefix(path, "/files/") {
			// Very basic body extraction: assumes body is everything after "\r\n\r\n"
			bodyParts := strings.SplitN(request, "\r\n\r\n", 2)
			requestBody := ""
			if len(bodyParts) == 2 {
				requestBody = bodyParts[1]
			}
			response = postFiles(path, requestBody)
		}
		// else: response remains 404

	default:
		response = "HTTP/1.1 405 Method Not Allowed\r\n\r\n"
	}
	// --- End Routing ---

	// Write the response back to the client
	_, writeErr := conn.Write([]byte(response))
	if writeErr != nil {
		log.Printf("Error writing response: %v", writeErr)
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
	defer l.Close() // Ensure listener is closed when main exits
	log.Printf("Server listening on %s...", addr)
	log.Printf("Serving files from directory: %s", serveDirectory)

	// Accept connections in a loop
	for {
		conn, err := l.Accept()
		if err != nil {
			// Log accept errors but continue if possible (e.g., temporary errors)
			// Exit might be too drastic unless it's a fatal listener error.
			log.Printf("Error accepting connection: %v", err)
			// Consider adding a small delay or check error type before continuing
			continue
		}
		// Handle each connection concurrently in a goroutine
		go do(conn) // Pass only the connection
	}
}
