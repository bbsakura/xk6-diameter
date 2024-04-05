package diameter

import "fmt"

type ErrNotFound struct {
	Name string
}

func (e *ErrNotFound) Error() string {
	return fmt.Sprintf("code not found `%s`", e.Name)
}

type ErrNoValue struct {
	Key string
}

func (e *ErrNoValue) Error() string {
	return fmt.Sprintf("no value : `%s`", e.Key)
}

type ErrInvalidType struct {
	Value interface{}
	Want  string
}

func (e *ErrInvalidType) Error() string {
	return fmt.Sprintf("invalid type(%v): want: %s, got: %s", e.Value, e.Want, e.Value)
}
