package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
)

type Message struct {
	From    string `json:"from"`
	To      string `json:"to"`
	Content string `json:"content"`
}

func main() {
	// Create shared dirs
	sharedDir := "./shared"
	subdirs := []string{"sockets/control", "sources", "builds", "outputs", "logs", "audit"}
	for _, sub := range subdirs {
		path := filepath.Join(sharedDir, sub)
		os.MkdirAll(path, 0755)
	}

	// Listen on TCP for message-hub
	listener, err := net.Listen("tcp", ":50023")
	if err != nil {
		log.Fatal(err)
	}
	defer listener.Close()
	log.Println("Seedclaw listening on :50023")

	// Start core skills via docker compose
	log.Println("Starting core skills...")
	err = exec.Command("docker", "compose", "up").Start()
	if err != nil {
		log.Println("Failed to start core skills:", err)
	}

	// Accept connection from message-hub
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			log.Println(err)
			return
		}
		log.Println("Message-hub connected")
		defer conn.Close()

		// Handle messages
		scanner := bufio.NewScanner(conn)
		for scanner.Scan() {
			var msg Message
			if err := json.Unmarshal(scanner.Bytes(), &msg); err != nil {
				log.Println("Invalid message:", err)
				continue
			}
			log.Printf("Received: %+v\n", msg)
			// Echo back
			response := Message{From: "seedclaw", To: msg.From, Content: "Echo: " + msg.Content}
			data, _ := json.Marshal(response)
			conn.Write(append(data, '\n'))
			log.Printf("Sent: %+v\n", response)
			// Send test message to llm-caller
			testMsg := Message{From: "seedclaw", To: "llm-caller", Content: "Say hello"}
			data2, _ := json.Marshal(testMsg)
			conn.Write(append(data2, '\n'))
			log.Printf("Sent test to llm-caller: %+v\n", testMsg)
		}
	}()

	// Stdin loop
	scanner := bufio.NewScanner(os.Stdin)
	fmt.Println("Enter messages to send to message-hub:")
	for {
		fmt.Print("> ")
		if !scanner.Scan() {
			break
		}
		input := scanner.Text()
		// For now, since no connection handle, just log
		log.Println("User input:", input)
		// To send, need to have the conn
		// This is a simplification; in real, need to send via the connection
	}
}
