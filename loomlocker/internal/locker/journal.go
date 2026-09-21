package locker

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	icrypto "github.com/sayandeep14/PromptLoom/loomlocker/internal/crypto"
)

// JournalFilename is the encrypted recovery file used in recoverable mode.
const JournalFilename = ".loom.secret.lock"

// ErrWrongPassword is returned when a journal cannot be decrypted with the password.
var ErrWrongPassword = errors.New("wrong password (or the recovery file is corrupt)")

var journalMagic = []byte("LOOMJRNL1")

const journalSaltLen = 16

// WriteJournal saves mapping, encrypted with key, atomically and with mode 0600.
// salt is the Argon2id salt the key was derived with; it is stored in the clear so the
// key can be re-derived from the password during recovery.
func WriteJournal(path string, key, salt []byte, mapping map[string]map[string]string) error {
	if len(salt) != journalSaltLen {
		return fmt.Errorf("journal salt must be %d bytes", journalSaltLen)
	}
	plain, err := json.Marshal(mapping)
	if err != nil {
		return err
	}
	sealed, err := icrypto.Encrypt(plain, key)
	if err != nil {
		return err
	}
	var buf bytes.Buffer
	buf.Write(journalMagic)
	buf.Write(salt)
	buf.Write(sealed)
	return writeFileAtomic(path, buf.Bytes(), 0o600)
}

// ReadJournal decrypts the journal at path with password.
func ReadJournal(path, password string) (map[string]map[string]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(data) < len(journalMagic)+journalSaltLen || !bytes.HasPrefix(data, journalMagic) {
		return nil, fmt.Errorf("%s is not a loomlocker recovery file", filepath.Base(path))
	}
	salt := data[len(journalMagic) : len(journalMagic)+journalSaltLen]
	key, _, err := icrypto.DeriveKey(password, salt)
	if err != nil {
		return nil, err
	}
	plain, err := icrypto.Decrypt(data[len(journalMagic)+journalSaltLen:], key)
	if err != nil {
		return nil, ErrWrongPassword
	}
	var mapping map[string]map[string]string
	if err := json.Unmarshal(plain, &mapping); err != nil {
		return nil, fmt.Errorf("recovery file is corrupt: %w", err)
	}
	return mapping, nil
}
