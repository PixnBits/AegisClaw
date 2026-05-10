package main

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"os"

	"github.com/spf13/cobra"
)

var key = []byte("examplekey123456789012345678901234") // 32 bytes for AES-256

func encrypt(data []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return gcm.Seal(nonce, nonce, data, nil), nil
}

func decrypt(data []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonceSize := gcm.NonceSize()
	if len(data) < nonceSize {
		return nil, fmt.Errorf("invalid data")
	}
	nonce, ciphertext := data[:nonceSize], data[nonceSize:]
	return gcm.Open(nil, nonce, ciphertext, nil)
}

func loadSecrets() map[string]string {
	secrets := make(map[string]string)
	file, err := os.Open("secrets.enc")
	if err != nil {
		return secrets
	}
	defer file.Close()
	data, err := ioutil.ReadAll(file)
	if err != nil {
		return secrets
	}
	decrypted, err := decrypt(data)
	if err != nil {
		log.Println("Failed to decrypt:", err)
		return secrets
	}
	json.Unmarshal(decrypted, &secrets)
	return secrets
}

func saveSecrets(secrets map[string]string) {
	data, _ := json.Marshal(secrets)
	encrypted, _ := encrypt(data)
	ioutil.WriteFile("secrets.enc", encrypted, 0600)
}

func setSecret(cmd *cobra.Command, args []string) {
	if len(args) != 2 {
		fmt.Println("Usage: secrets set <key> <value>")
		return
	}
	key, value := args[0], args[1]
	secrets := loadSecrets()
	secrets[key] = value
	saveSecrets(secrets)
	fmt.Println("Secret set")
}

func listSecrets(cmd *cobra.Command, args []string) {
	secrets := loadSecrets()
	for k := range secrets {
		fmt.Println(k)
	}
}

func removeSecret(cmd *cobra.Command, args []string) {
	if len(args) != 1 {
		fmt.Println("Usage: secrets remove <key>")
		return
	}
	key := args[0]
	secrets := loadSecrets()
	delete(secrets, key)
	saveSecrets(secrets)
	fmt.Println("Secret removed")
}

func main() {
	var rootCmd = &cobra.Command{Use: "secrets"}

	var setCmd = &cobra.Command{
		Use:   "set",
		Short: "Set a secret",
		Run:   setSecret,
	}

	var listCmd = &cobra.Command{
		Use:   "list",
		Short: "List secrets",
		Run:   listSecrets,
	}

	var removeCmd = &cobra.Command{
		Use:   "remove",
		Short: "Remove a secret",
		Run:   removeSecret,
	}

	rootCmd.AddCommand(setCmd)
	rootCmd.AddCommand(listCmd)
	rootCmd.AddCommand(removeCmd)

	rootCmd.Execute()
}
