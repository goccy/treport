package treport

import "fmt"

type InvalidRepositoryPathError struct {
	Path string
}

func (e *InvalidRepositoryPathError) Error() string {
	return fmt.Sprintf("invalid repository path: %q", e.Path)
}

func ErrInvalidRepositoryPath(path string) error {
	return &InvalidRepositoryPathError{
		Path: path,
	}
}
