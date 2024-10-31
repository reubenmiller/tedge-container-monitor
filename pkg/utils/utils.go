package utils

import (
	"errors"
	"os"
)

func PathExists(p string) bool {
	_, error := os.Stat(p)
	return !errors.Is(error, os.ErrNotExist)
}

func CopyFile(src string, dst string) error {
	// Read all content of src to data, may cause OOM for a large file.
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	// Write data to dst
	err = os.WriteFile(dst, data, 0644)
	return err
}
