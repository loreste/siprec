package database

import "errors"

// ErrMySQLDisabled indicates that the binary was built without MySQL support.
var ErrMySQLDisabled = errors.New("mysql support not enabled; rebuild with -tags mysql")

