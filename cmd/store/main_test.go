package main

import (
	"os"
	"testing"
)

func TestLoadFromFile(t *testing.T) {
	data := loadFromFile("nonexistent.json")
	if len(data) != 0 {
		t.Error("Expected empty map for nonexistent file")
	}
}

func TestSaveToFile(t *testing.T) {
	data := map[string]interface{}{"test": "value"}
	saveToFile("test_skills.json", data)
	defer os.Remove("test_skills.json")

	loaded := loadFromFile("test_skills.json")
	if loaded["test"] != "value" {
		t.Errorf("Expected value, got %v", loaded["test"])
	}
}