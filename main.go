package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync/atomic"

	"github.com/pkg/errors"
)

// ------ Error handling ------

// Incrementing integer to generate unique error numbers
var errNo uint64

// API error struct contains information about the error to send to the client
type apiError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	errno   uint64
	err     error
}

func (e apiError) Error() string {
	if e.err != nil {
		return fmt.Sprintf("%s: %v", e.Message, e.err)
	}
	return e.Message
}

func (e apiError) F(a ...any) apiError {
	e.Message = fmt.Sprintf(e.Message, a...)
	return e
}

func (e apiError) Wrap(err error) error {
	e.err = err
	return e
}

func (e apiError) Unwrap() error {
	return e.err
}

func (e apiError) Is(target error) bool {
	// Check if error is of type apiError
	if t, ok := target.(apiError); ok {
		return e.errno == t.errno
	}
	// Else check underlying error
	return errors.Is(e.err, target)
}

func NewError(code int, message string) apiError {
	err := apiError{
		Code:    code,
		Message: message,
		errno:   errNo,
	}
	atomic.AddUint64(&errNo, 1)
	return err
}

// ------ Database ------

// API responses are defined together with the error - error is always handled in the api layer
// Additional information about the error can be added to the error struct (entity name, ...)
//
// Errors follow a naming convention (Err{Entity}{Reason}) and are defined in the package where they are used (database, service, api)
// There are two possible options for defining errors:
//
//  1. Generic errors errors (ErrNotFound, ErrInvalidId, ...) (current implementation)
//     - one error for each type of error
//     - pass entity name as a parameter to the error and dinamically create the error message:
//     var ErrNotFound = NewError(http.StatusNotFound, "%s not found")
//     ----------------------------------
//     return ErrNotFound.F("thing") // result: "thing not found"
//
//  2. Specific errors (ErrThingNotFound, ErrThingInvalidId, ...)
//     - cleaner code (no Error.F() calls)
//     - more errors to define
//     - can lead to bugs when checking for errors in upper layers

var (
	// Database layer errors
	ErrNotFound  = NewError(http.StatusNotFound, "%s not found")
	ErrInvalidId = NewError(http.StatusBadRequest, "invalid %s id")

	// Error returned from external library (db client)
	ErrFromExternalLib = errors.New("external lib error")
)

type Database struct{}

// Simulate error returned from external library
func externalLibGet() error {
	return ErrFromExternalLib
}

func (Database) GetThingById(id int) error {
	if id < 0 {
		// Database error
		return ErrInvalidId.F("thing")
	}

	err := externalLibGet()
	if err != nil {
		return ErrNotFound.F("thing").Wrap(err)
	}

	return nil
}

// ------ Service ------

// Service layer errors
var ErrThingIdTooHigh = NewError(http.StatusBadRequest, "%s id too high")

type Service struct {
	db Database
}

func (s Service) DoSomethingWithThing(id int) error {
	if id >= 10 {
		// Bussiness logic error
		return ErrThingIdTooHigh.F("thing")
	}

	err := s.db.GetThingById(id)
	if err != nil {

		switch {
		case errors.Is(err, ErrInvalidId):
			// Optionally wrap specific errors with additional context (only for debugging, not visible in response)
			return errors.Wrap(err, "this is a wrapped error")
		}
		return err
	}

	return nil
}

// ------ API ------

type API struct {
	service Service
}

func respondWithError(err error) {
	var apiErr apiError
	// errors.As() checks if the error is of type apiError and assigns it to apiErr variable
	if errors.As(err, &apiErr) {
		// Print error for debugging (only for demo, usually in service layer)
		fmt.Printf("Internal error: %v\n", err)

		// Marshal and send to client
		body, _ := json.MarshalIndent(apiErr, "", "  ")
		fmt.Println(string(body))
		fmt.Println()
		return
	}

	// Handle unknown errors
	// Send 500 internal server error
	fmt.Printf("Responding with unknown error: %+v\n", err)
}

// This is called when the client requests /things/{id}
func (a API) DoSomethingWithThingHandler(id int) {
	err := a.service.DoSomethingWithThing(id)
	if err != nil {
		respondWithError(err)
		return
	}

	// Send 200 OK
}

func main() {
	api := API{
		service: Service{
			db: Database{},
		},
	}

	// Simulate client requests
	api.DoSomethingWithThingHandler(10)
	api.DoSomethingWithThingHandler(5)
	api.DoSomethingWithThingHandler(-1)

	// Output:
	/*
		Internal error: thing id too high
		{
			"code": 400,
			"message": "thing id too high"
		}

		Internal error: thing not found: external lib error
		{
			"code": 404,
			"message": "thing not found"
		}

		Internal error: this is a wrapped error: invalid thing id
		{
			"code": 400,
			"message": "invalid thing id"
		}
	*/
}
