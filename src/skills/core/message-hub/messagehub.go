package main

import (
	"bufio"
	"encoding/json"
	"log"
	"net"
	"os"
	"sync"
)

type Message struct {
	From    string `json:"from"`
	To      string `json:"to"`
	Content string `json:"content"`
}

var mu sync.Mutex
var connections = make(map[string]net.Conn)

func main() {
	addr := os.Getenv("CONTROL_ADDR")
	if addr == "" {
		addr = "host.docker.internal:50023"
	}

	conn, err := net.Dial("tcp", addr)
	if err != nil {
		log.Fatal(err)
	}
	defer conn.Close()
	log.Println("Connected to seedclaw at", addr)

	mu.Lock()
	connections["seedclaw"] = conn
	mu.Unlock()

	// Send hello
	msg := Message{From: "message-hub", To: "seedclaw", Content: "Hello from message-hub"}
	data, _ := json.Marshal(msg)
	conn.Write(append(data, '\n'))

	// Start listening for skills
	listener, err := net.Listen("tcp", ":50024")
	if err != nil {
		log.Fatal(err)
	}
	defer listener.Close()
	log.Println("Listening for skills on :50024")

	go func() {
		for {
			skillConn, err := listener.Accept()
			if err != nil {
				log.Println("Accept error:", err)
				continue
			}
			go handleSkill(skillConn)
		}
	}()

	// Listen for responses from seedclaw
	scanner := bufio.NewScanner(conn)
	for scanner.Scan() {
		var msg Message
		if err := json.Unmarshal(scanner.Bytes(), &msg); err != nil {
			log.Println("Invalid message:", err)
			continue
		}
		log.Printf("Received from seedclaw: %+v\n", msg)
		routeMessage(msg)
	}
}

func handleSkill(conn net.Conn) {
	defer conn.Close()
	scanner := bufio.NewScanner(conn)
	if !scanner.Scan() {
		return
	}
	var regMsg Message
	if err := json.Unmarshal(scanner.Bytes(), &regMsg); err != nil {
		log.Println("Invalid register message:", err)
		return
	}
	if regMsg.Content != "register" {
		log.Println("Expected register, got:", regMsg)
		return
	}
	name := regMsg.From
	mu.Lock()
	connections[name] = conn
	mu.Unlock()
	log.Println("Registered skill:", name)

	for scanner.Scan() {
		var msg Message
		if err := json.Unmarshal(scanner.Bytes(), &msg); err != nil {
			log.Println("Invalid message:", err)
			continue
		}
		log.Printf("Received from %s: %+v\n", name, msg)
		routeMessage(msg)
	}

	mu.Lock()
	delete(connections, name)
	mu.Unlock()
	log.Println("Unregistered skill:", name)
}

func routeMessage(msg Message) {
	mu.Lock()
	target, ok := connections[msg.To]
	mu.Unlock()
	if !ok {
		log.Println("No connection for:", msg.To)
		return
	}
	data, _ := json.Marshal(msg)
	target.Write(append(data, '\n'))
}
