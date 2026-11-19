package config

// Error reports that a configuration cannot serve the request: the file is
// missing or unreadable, its YAML or schema is invalid, a reference does not
// resolve, or the requested flow is not declared. Callers detect it with
// errors.As to tell "the config or invocation is wrong" apart from "the work
// itself failed"; the CLI maps it to exit code 2.
//
// Error is transparent: its message and chain are those of the wrapped error.
type Error struct {
	Err error
}

func (e *Error) Error() string { return e.Err.Error() }

func (e *Error) Unwrap() error { return e.Err }

// invalid marks err as a configuration error. A nil err stays nil so call
// sites can wrap a return value unconditionally.
func invalid(err error) error {
	if err == nil {
		return nil
	}
	return &Error{Err: err}
}
