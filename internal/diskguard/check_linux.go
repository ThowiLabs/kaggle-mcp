//go:build linux

package diskguard

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

func Check(path string) error {
	p := path
	for {
		if _, err := os.Stat(p); err == nil {
			break
		}
		next := filepath.Dir(p)
		if next == p {
			return errors.New("sin ruta de volumen")
		}
		p = next
	}
	var st syscall.Statfs_t
	if err := syscall.Statfs(p, &st); err != nil {
		return err
	}
	free := uint64(st.Bavail) * uint64(st.Bsize)
	total := uint64(st.Blocks) * uint64(st.Bsize)
	if total == 0 {
		return errors.New("volumen sin capacidad")
	}
	if free < 512<<20 || free*100/total < 3 {
		return fmt.Errorf("ALMACENAMIENTO CRÍTICO: quedan %d MiB libres. No escribir ni instalar; Kaggle puede quedar readonly", free>>20)
	}
	return nil
}
