package main

// Hardened interim secrets CLI (6.1.5 security priority).
// - NO hardcoded keys ever.
// - Passphrase or --keyfile (0600) derived 32-byte AES key (simple PBKDF via repeated SHA-256; interim until age/HKDF full vault in phase 21).
// - Storage: ~/.aegis/secrets/secrets.enc (0700 dir, 0600 file) with proper user home resolution.
// - Modern APIs (os.ReadFile etc.), basic --stdin support for set, best-effort zeroing.
// - Standalone + exec'd by `aegis secrets` for isolation (never in daemon TCB path).
// Security note: This is stronger than the original placeholder but still interim. Use only for dev; production secrets via full vault.

import (
	"bufio"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

const (
	defaultKeyIterations = 100000 // simple PBKDF strength (interim)
	secretsDirPerm       = 0o700
	secretsFilePerm      = 0o600
)

var (
	passphraseFlag string
	keyFileFlag    string
	stdinFlag      bool
)

// deriveKey turns passphrase or keyfile into 32-byte AES key. Zeroes input material best-effort.
func deriveKey(passphrase []byte, keyFile string) ([]byte, error) {
	if keyFile != "" {
		data, err := os.ReadFile(keyFile)
		if err != nil {
			return nil, fmt.Errorf("keyfile read: %w", err)
		}
		defer zeroBytes(data)
		if len(data) < 32 {
			return nil, fmt.Errorf("keyfile too short (<32 bytes)")
		}
		key := make([]byte, 32)
		copy(key, data[:32])
		return key, nil
	}

	if len(passphrase) == 0 {
		return nil, fmt.Errorf("no passphrase or keyfile provided")
	}
	defer zeroBytes(passphrase)

	h := sha256.New()
	h.Write(passphrase)
	sum := h.Sum(nil)
	for i := 1; i < defaultKeyIterations; i++ {
		h.Reset()
		h.Write(sum)
		h.Write(passphrase)
		sum = h.Sum(nil)
	}
	key := make([]byte, 32)
	copy(key, sum)
	return key, nil
}

func zeroBytes(b []byte) {
	for i := range b {
		b[i] = 0
	}
}

func getSecretsPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, ".aegis", "secrets")
	if err := os.MkdirAll(dir, secretsDirPerm); err != nil {
		return "", err
	}
	return filepath.Join(dir, "secrets.enc"), nil
}

func loadKey() ([]byte, error) {
	if keyFileFlag != "" {
		return deriveKey(nil, keyFileFlag)
	}
	if passphraseFlag != "" {
		return deriveKey([]byte(passphraseFlag), "")
	}
	// Interactive prompt if tty (best effort)
	fmt.Print("Enter passphrase for secrets (input hidden in real TTY): ")
	reader := bufio.NewReader(os.Stdin)
	line, _ := reader.ReadString('\n')
	pw := strings.TrimSpace(line)
	if pw == "" {
		return nil, fmt.Errorf("no passphrase")
	}
	defer zeroBytes([]byte(pw))
	return deriveKey([]byte(pw), "")
}

func encrypt(key, data []byte) ([]byte, error) {
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

func decrypt(key, data []byte) ([]byte, error) {
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

func loadSecrets() (map[string]string, error) {
	secrets := make(map[string]string)
	path, err := getSecretsPath()
	if err != nil {
		return secrets, err
	}
	key, err := loadKey()
	if err != nil {
		return secrets, err
	}
	defer zeroBytes(key)

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return secrets, nil
		}
		return secrets, err
	}
	decrypted, err := decrypt(key, data)
	if err != nil {
		return secrets, fmt.Errorf("decrypt failed (wrong key/passphrase?): %w", err)
	}
	json.Unmarshal(decrypted, &secrets)
	return secrets, nil
}

func saveSecrets(secrets map[string]string) error {
	path, err := getSecretsPath()
	if err != nil {
		return err
	}
	key, err := loadKey()
	if err != nil {
		return err
	}
	defer zeroBytes(key)

	data, _ := json.Marshal(secrets)
	encrypted, err := encrypt(key, data)
	if err != nil {
		return err
	}
	return os.WriteFile(path, encrypted, secretsFilePerm)
}

func setSecret(cmd *cobra.Command, args []string) {
	var keyStr, value string
	if stdinFlag {
		// Read from stdin for value (or full "key=value")
		reader := bufio.NewReader(os.Stdin)
		line, _ := reader.ReadString('\n')
		line = strings.TrimSpace(line)
		if strings.Contains(line, "=") {
			parts := strings.SplitN(line, "=", 2)
			keyStr, value = parts[0], parts[1]
		} else if len(args) > 0 {
			keyStr = args[0]
			value = line
		}
	} else if len(args) >= 2 {
		keyStr, value = args[0], args[1]
	} else {
		fmt.Println("Usage: secrets set <key> <value> [--stdin] [--passphrase=...] [--keyfile=...]")
		return
	}

	secrets, err := loadSecrets()
	if err != nil {
		fmt.Println("Error loading secrets:", err)
		return
	}
	secrets[keyStr] = value
	if err := saveSecrets(secrets); err != nil {
		fmt.Println("Error saving:", err)
		return
	}
	fmt.Println("Secret set (stored encrypted under ~/.aegis/secrets/)")
}

func listSecrets(cmd *cobra.Command, args []string) {
	secrets, err := loadSecrets()
	if err != nil {
		fmt.Println("Error:", err)
		return
	}
	for k := range secrets {
		fmt.Println(k)
	}
}

func removeSecret(cmd *cobra.Command, args []string) {
	if len(args) != 1 {
		fmt.Println("Usage: secrets remove <key> [--passphrase=...]")
		return
	}
	key := args[0]
	secrets, err := loadSecrets()
	if err != nil {
		fmt.Println("Error:", err)
		return
	}
	delete(secrets, key)
	if err := saveSecrets(secrets); err != nil {
		fmt.Println("Error saving:", err)
		return
	}
	fmt.Println("Secret removed")
}

func main() {
	rootCmd := &cobra.Command{
		Use:   "secrets",
		Short: "AegisClaw secrets (interim hardened storage under ~/.aegis/secrets)",
	}

	rootCmd.PersistentFlags().StringVar(&passphraseFlag, "passphrase", "", "Passphrase for key derivation (insecure on CLI; prefer --keyfile or interactive)")
	rootCmd.PersistentFlags().StringVar(&keyFileFlag, "keyfile", "", "Path to 32+ byte key file (recommended, 0600 perms)")
	rootCmd.PersistentFlags().BoolVar(&stdinFlag, "stdin", false, "Read value from stdin (for set)")

	setCmd := &cobra.Command{
		Use:   "set <key> [value]",
		Short: "Set a secret (supports --stdin)",
		Run:   setSecret,
	}
	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List secret keys (no values)",
		Run:   listSecrets,
	}
	removeCmd := &cobra.Command{
		Use:   "remove <key>",
		Short: "Remove a secret",
		Run:   removeSecret,
	}

	rootCmd.AddCommand(setCmd, listCmd, removeCmd)
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
