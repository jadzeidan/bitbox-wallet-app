// SPDX-License-Identifier: Apache-2.0

package main

import (
	"archive/tar"
	"compress/gzip"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
)

type authOutcome int

const (
	authOutcomeOK authOutcome = iota
	authOutcomeCancel
	authOutcomeMissing
	authOutcomeError
)

type nativeAuthFunc func(reason string) authOutcome

const (
	protectedStateFilename = "appdata-protection-state.json"
	protectedBlobFilename  = "appdata-protected.bin"
	protectedMagic         = "BBVAULT2"
	keychainService        = "ch.shiftcrypto.bitbox.appdata-key"
	keychainAccount        = "default"
	chunkSize              = 1024 * 1024
)

var unprotectedTopLevelEntries = map[string]struct{}{
	"log.txt":                          {},
	"log.txt.1":                        {},
	protectedStateFilename:             {},
	protectedBlobFilename:              {},
	".DS_Store":                        {},
	".com.apple.timemachine.supported": {},
}

type protectedState struct {
	Enabled bool `json:"enabled"`
}

type appDataProtection struct {
	log      *logrus.Entry
	baseDir  string
	dataDir  string
	state    protectedState
	auth     nativeAuthFunc
	key      []byte
	runtime  string
	blobPath string
}

func setupAppDataProtection(baseDir string, auth nativeAuthFunc, log *logrus.Entry) (*appDataProtection, error) {
	manager := &appDataProtection{
		log:      log.WithField("group", "data-protection"),
		baseDir:  baseDir,
		dataDir:  baseDir,
		auth:     auth,
		blobPath: filepath.Join(baseDir, protectedBlobFilename),
	}
	manager.state = manager.readState()
	if runtime.GOOS != "darwin" || !manager.state.Enabled {
		return manager, nil
	}

	if result := manager.auth("Authenticate to unlock BitBoxApp"); result != authOutcomeOK {
		return nil, fmt.Errorf("startup authentication failed: %v", result)
	}

	key, err := manager.loadOrCreateKey()
	if err != nil {
		return nil, err
	}
	manager.key = key

	runtimeDir, err := os.MkdirTemp("", "bitbox-protected-*")
	if err != nil {
		return nil, err
	}
	manager.runtime = runtimeDir
	manager.dataDir = runtimeDir

	if _, err := os.Stat(manager.blobPath); err == nil {
		if err := decryptArchiveToDir(manager.blobPath, runtimeDir, key); err != nil {
			return nil, err
		}
	} else {
		if err := copyProtectedEntries(manager.baseDir, runtimeDir); err != nil {
			return nil, err
		}
	}
	return manager, nil
}

func (manager *appDataProtection) readState() protectedState {
	data, err := os.ReadFile(filepath.Join(manager.baseDir, protectedStateFilename))
	if err != nil {
		return protectedState{Enabled: false}
	}
	var state protectedState
	if err := json.Unmarshal(data, &state); err != nil {
		return protectedState{Enabled: false}
	}
	return state
}

func (manager *appDataProtection) writeState(enabled bool) {
	manager.state.Enabled = enabled
	data, err := json.Marshal(manager.state)
	if err != nil {
		manager.log.WithError(err).Error("could not marshal data protection state")
		return
	}
	if err := os.WriteFile(filepath.Join(manager.baseDir, protectedStateFilename), data, 0600); err != nil {
		manager.log.WithError(err).Error("could not persist data protection state")
	}
}

func (manager *appDataProtection) onAuthSettingChanged(enabled bool) {
	if runtime.GOOS != "darwin" || enabled == manager.state.Enabled {
		return
	}

	if enabled {
		result := manager.auth("Authenticate to enable app data protection")
		if result != authOutcomeOK {
			manager.log.WithField("auth-result", result).Warn("enable app data protection auth failed")
			return
		}
		key, err := manager.loadOrCreateKey()
		if err != nil {
			manager.log.WithError(err).Error("could not initialize data protection key")
			return
		}
		manager.key = key
	}
	manager.writeState(enabled)
}

func (manager *appDataProtection) close() {
	if runtime.GOOS != "darwin" {
		return
	}

	defer func() {
		if manager.runtime != "" {
			if err := os.RemoveAll(manager.runtime); err != nil {
				manager.log.WithError(err).Warn("could not remove temporary runtime data dir")
			}
		}
	}()

	if manager.runtime == "" {
		if !manager.state.Enabled {
			_ = os.Remove(manager.blobPath)
			_ = manager.deleteKey()
			return
		}
		key, err := manager.loadOrCreateKey()
		if err != nil {
			manager.log.WithError(err).Error("could not load key while enabling protection")
			return
		}
		startedAt := time.Now()
		if err := encryptDirToArchive(manager.baseDir, manager.blobPath, key, true); err != nil {
			manager.log.WithError(err).Error("could not encrypt base app data")
			return
		}
		manager.log.WithField("duration", time.Since(startedAt).String()).Info("encrypted base app data")
		if err := clearProtectedEntries(manager.baseDir); err != nil {
			manager.log.WithError(err).Warn("could not clear unencrypted base app data")
		}
		return
	}

	if manager.state.Enabled {
		key := manager.key
		if len(key) == 0 {
			var err error
			key, err = manager.loadOrCreateKey()
			if err != nil {
				manager.log.WithError(err).Error("could not load key while closing protected runtime")
				return
			}
		}
		startedAt := time.Now()
		if err := encryptDirToArchive(manager.runtime, manager.blobPath, key, false); err != nil {
			manager.log.WithError(err).Error("could not encrypt protected runtime")
			return
		}
		manager.log.WithField("duration", time.Since(startedAt).String()).Info("encrypted protected runtime data")
		if err := clearProtectedEntries(manager.baseDir); err != nil {
			manager.log.WithError(err).Warn("could not clear unencrypted base app data")
		}
		return
	}

	if err := clearProtectedEntries(manager.baseDir); err != nil {
		manager.log.WithError(err).Warn("could not clear base dir while disabling protection")
	}
	if err := copyAllEntries(manager.runtime, manager.baseDir); err != nil {
		manager.log.WithError(err).Error("could not restore plaintext app data while disabling protection")
	}
	_ = os.Remove(manager.blobPath)
	_ = manager.deleteKey()
}

func (manager *appDataProtection) loadOrCreateKey() ([]byte, error) {
	key, err := manager.loadKey()
	if err == nil {
		return key, nil
	}
	key = make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	if err := manager.storeKey(key); err != nil {
		return nil, err
	}
	return key, nil
}

func (manager *appDataProtection) loadKey() ([]byte, error) {
	out, err := exec.Command("security", "find-generic-password", "-a", keychainAccount, "-s", keychainService, "-w").Output()
	if err != nil {
		return nil, err
	}
	key, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(out)))
	if err != nil {
		return nil, err
	}
	if len(key) != 32 {
		return nil, fmt.Errorf("invalid key length %d", len(key))
	}
	return key, nil
}

func (manager *appDataProtection) storeKey(key []byte) error {
	value := base64.StdEncoding.EncodeToString(key)
	return exec.Command("security", "add-generic-password", "-a", keychainAccount, "-s", keychainService, "-w", value, "-U").Run()
}

func (manager *appDataProtection) deleteKey() error {
	return exec.Command("security", "delete-generic-password", "-a", keychainAccount, "-s", keychainService).Run()
}

func encryptDirToArchive(sourceDir, archivePath string, key []byte, skipUnprotected bool) error {
	tmpPath := archivePath + ".tmp"
	out, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer out.Close() //nolint:errcheck
	if _, err := out.Write([]byte(protectedMagic)); err != nil {
		return err
	}
	prefix := make([]byte, 8)
	if _, err := rand.Read(prefix); err != nil {
		return err
	}
	if _, err := out.Write(prefix); err != nil {
		return err
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return err
	}

	reader, writer := io.Pipe()
	errChan := make(chan error, 1)
	go func() {
		errChan <- streamTarGz(sourceDir, writer, skipUnprotected)
	}()

	buf := make([]byte, chunkSize)
	counter := uint32(0)
	for {
		n, err := reader.Read(buf)
		if n > 0 {
			nonce := make([]byte, 12)
			copy(nonce, prefix)
			binary.BigEndian.PutUint32(nonce[8:], counter)
			counter++
			ciphertext := gcm.Seal(nil, nonce, buf[:n], nil)
			if err := binary.Write(out, binary.BigEndian, uint32(len(ciphertext))); err != nil {
				return err
			}
			if _, err := out.Write(ciphertext); err != nil {
				return err
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
	}
	if err := <-errChan; err != nil {
		return err
	}

	if err := binary.Write(out, binary.BigEndian, uint32(0)); err != nil {
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, archivePath)
}

func decryptArchiveToDir(archivePath, targetDir string, key []byte) error {
	in, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer in.Close() //nolint:errcheck

	magic := make([]byte, len(protectedMagic))
	if _, err := io.ReadFull(in, magic); err != nil {
		return err
	}
	if string(magic) != protectedMagic {
		return fmt.Errorf("invalid protected data file")
	}
	prefix := make([]byte, 8)
	if _, err := io.ReadFull(in, prefix); err != nil {
		return err
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return err
	}

	reader, writer := io.Pipe()
	errChan := make(chan error, 1)
	go func() {
		defer writer.Close() //nolint:errcheck
		counter := uint32(0)
		for {
			var encryptedChunkLen uint32
			if err := binary.Read(in, binary.BigEndian, &encryptedChunkLen); err != nil {
				if err == io.EOF {
					errChan <- nil
					return
				}
				errChan <- err
				return
			}
			if encryptedChunkLen == 0 {
				errChan <- nil
				return
			}
			encryptedChunk := make([]byte, encryptedChunkLen)
			if _, err := io.ReadFull(in, encryptedChunk); err != nil {
				errChan <- err
				return
			}
			nonce := make([]byte, 12)
			copy(nonce, prefix)
			binary.BigEndian.PutUint32(nonce[8:], counter)
			counter++
			plaintext, err := gcm.Open(nil, nonce, encryptedChunk, nil)
			if err != nil {
				errChan <- err
				return
			}
			if _, err := writer.Write(plaintext); err != nil {
				errChan <- err
				return
			}
		}
	}()

	if err := extractTarGz(reader, targetDir); err != nil {
		return err
	}
	return <-errChan
}

func streamTarGz(root string, writer *io.PipeWriter, skipUnprotected bool) error {
	defer writer.Close() //nolint:errcheck
	gzipWriter, err := gzip.NewWriterLevel(writer, gzip.BestSpeed)
	if err != nil {
		return err
	}
	defer gzipWriter.Close() //nolint:errcheck
	tarWriter := tar.NewWriter(gzipWriter)
	defer tarWriter.Close() //nolint:errcheck

	return filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == root {
			return nil
		}

		relPath, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		relPath = filepath.ToSlash(relPath)
		topLevel := strings.Split(relPath, "/")[0]
		if skipUnprotected {
			if _, ok := unprotectedTopLevelEntries[topLevel]; ok {
				if d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
		}

		info, err := d.Info()
		if err != nil {
			return err
		}
		header, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		header.Name = relPath
		if info.IsDir() && !strings.HasSuffix(header.Name, "/") {
			header.Name += "/"
		}
		if err := tarWriter.WriteHeader(header); err != nil {
			return err
		}
		if info.Mode().IsRegular() {
			file, err := os.Open(path)
			if err != nil {
				return err
			}
			if _, err := io.Copy(tarWriter, file); err != nil {
				file.Close() //nolint:errcheck
				return err
			}
			if err := file.Close(); err != nil {
				return err
			}
		}
		return nil
	})
}

func extractTarGz(reader io.Reader, targetDir string) error {
	gzipReader, err := gzip.NewReader(reader)
	if err != nil {
		return err
	}
	defer gzipReader.Close() //nolint:errcheck

	tarReader := tar.NewReader(gzipReader)
	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		cleanName := filepath.Clean(header.Name)
		targetPath := filepath.Join(targetDir, cleanName)
		if !strings.HasPrefix(targetPath, targetDir+string(os.PathSeparator)) && targetPath != targetDir {
			return fmt.Errorf("invalid path in archive: %s", header.Name)
		}
		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(targetPath, 0700); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(targetPath), 0700); err != nil {
				return err
			}
			file, err := os.OpenFile(targetPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, os.FileMode(header.Mode))
			if err != nil {
				return err
			}
			if _, err := io.Copy(file, tarReader); err != nil {
				file.Close() //nolint:errcheck
				return err
			}
			if err := file.Close(); err != nil {
				return err
			}
		}
	}
}

func clearProtectedEntries(baseDir string) error {
	entries, err := os.ReadDir(baseDir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if _, ok := unprotectedTopLevelEntries[entry.Name()]; ok {
			continue
		}
		if err := os.RemoveAll(filepath.Join(baseDir, entry.Name())); err != nil {
			return err
		}
	}
	return nil
}

func copyProtectedEntries(srcDir, dstDir string) error {
	entries, err := os.ReadDir(srcDir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if _, ok := unprotectedTopLevelEntries[entry.Name()]; ok {
			continue
		}
		srcPath := filepath.Join(srcDir, entry.Name())
		dstPath := filepath.Join(dstDir, entry.Name())
		if err := copyEntry(srcPath, dstPath); err != nil {
			return err
		}
	}
	return nil
}

func copyAllEntries(srcDir, dstDir string) error {
	entries, err := os.ReadDir(srcDir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		srcPath := filepath.Join(srcDir, entry.Name())
		dstPath := filepath.Join(dstDir, entry.Name())
		if err := copyEntry(srcPath, dstPath); err != nil {
			return err
		}
	}
	return nil
}

func copyEntry(srcPath, dstPath string) error {
	info, err := os.Stat(srcPath)
	if err != nil {
		return err
	}
	if info.IsDir() {
		if err := os.MkdirAll(dstPath, 0700); err != nil {
			return err
		}
		entries, err := os.ReadDir(srcPath)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if err := copyEntry(filepath.Join(srcPath, entry.Name()), filepath.Join(dstPath, entry.Name())); err != nil {
				return err
			}
		}
		return nil
	}

	srcFile, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer srcFile.Close() //nolint:errcheck
	dstFile, err := os.OpenFile(dstPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, info.Mode())
	if err != nil {
		return err
	}
	if _, err := io.Copy(dstFile, srcFile); err != nil {
		dstFile.Close() //nolint:errcheck
		return err
	}
	return dstFile.Close()
}
