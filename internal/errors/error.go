package errors

import (
	"fmt"

	"golang.org/x/xerrors"
)

// Stack wrap error for stack trace
func Stack(err error) error {
	return &wrapError{
		baseError: &baseError{},
		err:       xerrors.Errorf(""),
		parentErr: err,
		frame:     xerrors.Caller(1),
	}
}

// Wrap wrap error with message for stack trace
func Wrap(err error, msg string) error {
	return &wrapError{
		baseError: &baseError{},
		err:       xerrors.Errorf(msg),
		parentErr: err,
		frame:     xerrors.Caller(1),
	}
}

// Wrapf wrap with message and args error for stack trace
func Wrapf(err error, msg string, args ...interface{}) error {
	return &wrapError{
		baseError: &baseError{},
		err:       xerrors.Errorf(msg, args...),
		parentErr: err,
		frame:     xerrors.Caller(1),
	}
}

type baseError struct {
	state fmt.State
	verb  rune
}

func (e *baseError) Error() string {
	return ""
}

func (e *baseError) chainStateAndVerb(err error) {
	wrapErr, ok := err.(*wrapError)
	if ok {
		wrapErr.state = e.state
		wrapErr.verb = e.verb
	}
}

type wrapError struct {
	*baseError
	err       error
	parentErr error
	frame     xerrors.Frame
}

func (e *wrapError) rootError() error {
	err := e.parentErr
	for {
		if wrapErr, ok := err.(*wrapError); ok {
			err = wrapErr.parentErr
			continue
		}
		break
	}
	return err
}

func (e *wrapError) As(target interface{}) bool {
	return xerrors.As(e.rootError(), target)
}

func (e *wrapError) Unwrap() error {
	return e.parentErr
}

func (e *wrapError) FormatError(p xerrors.Printer) error {
	if e.verb == 'v' && e.state.Flag('+') {
		// print stack trace for debugging
		p.Print(e.err)
		e.frame.Format(p)
		e.chainStateAndVerb(e.parentErr)
		return e.parentErr
	}
	err := e.rootError()
	e.chainStateAndVerb(err)
	if fmtErr, ok := err.(xerrors.Formatter); ok {
		fmtErr.FormatError(p)
	} else {
		p.Print(err)
	}
	return nil
}

func (e *wrapError) Format(state fmt.State, verb rune) {
	e.state = state
	e.verb = verb
	xerrors.FormatError(e, &wrapState{org: state}, verb)
}

func (e *wrapError) Error() string {
	return fmt.Sprintf("%v", e.err)
}

type wrapState struct {
	org fmt.State
}

func (s *wrapState) Write(b []byte) (n int, err error) {
	return s.org.Write(b)
}

func (s *wrapState) Width() (wid int, ok bool) {
	return s.org.Width()
}

func (s *wrapState) Precision() (prec int, ok bool) {
	return s.org.Precision()
}

func (s *wrapState) Flag(c int) bool {
	// set true to 'printDetail' forced because when p.Detail() is false, xerrors.Printer no output any text
	if c == '#' {
		// ignore '#' keyword because xerrors.FormatError doesn't set true to printDetail.
		// ( see https://github.com/golang/xerrors/blob/master/adaptor.go#L39-L43 )
		return false
	}
	return true
}
