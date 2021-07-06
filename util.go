package treport

import (
	"crypto/sha1"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func existsPath(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func mkdirIfNotExists(path string) error {
	if existsPath(path) {
		return nil
	}
	return os.MkdirAll(path, 0755)
}

func mkdirForClone(repoPath string) error {
	paths := strings.Split(repoPath, string(os.PathSeparator))
	cloneDir := filepath.Join(append([]string{"/"}, paths[:len(paths)-1]...)...)
	return mkdirIfNotExists(cloneDir)
}

func makeHashID(src string) string {
	hash := sha1.New()
	io.WriteString(hash, src)
	return fmt.Sprintf("%x", hash.Sum(nil))
}
