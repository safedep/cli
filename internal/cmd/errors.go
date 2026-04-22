package cmd

import "fmt"

func ErrNotImplemented(msg string) error {
	return fmt.Errorf("%s", msg)
}
