package portalstomp

import "strings"

const nullByte = "\x00"

// ParseFrame splits a STOMP frame (command + headers + body) from wire text.
func ParseFrame(raw string) (command string, headers map[string]string, body string, ok bool) {
	raw = strings.TrimSuffix(raw, nullByte)
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil, "", false
	}
	parts := strings.SplitN(raw, "\n\n", 2)
	head := parts[0]
	body = ""
	if len(parts) > 1 {
		body = parts[1]
	}
	lines := strings.Split(head, "\n")
	command = strings.TrimSpace(lines[0])
	headers = make(map[string]string)
	for _, line := range lines[1:] {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		k, v, found := strings.Cut(line, ":")
		if found {
			headers[strings.TrimSpace(k)] = strings.TrimSpace(v)
		}
	}
	return command, headers, body, command != ""
}

func BuildMessageFrame(destination, subscriptionID string, body []byte) string {
	var b strings.Builder
	b.WriteString("MESSAGE\n")
	b.WriteString("destination:")
	b.WriteString(destination)
	b.WriteString("\n")
	if subscriptionID != "" {
		b.WriteString("subscription:")
		b.WriteString(subscriptionID)
		b.WriteString("\n")
	}
	b.WriteString("content-type:application/json\n")
	b.WriteString("\n")
	b.Write(body)
	b.WriteString(nullByte)
	return b.String()
}

func BuildConnectedFrame() string {
	return "CONNECTED\nversion:1.2\nheart-beat:0,0\n\n" + nullByte
}

func BuildReceiptFrame(receiptID string) string {
	if receiptID == "" {
		return ""
	}
	return "RECEIPT\nreceipt-id:" + receiptID + "\n\n" + nullByte
}
