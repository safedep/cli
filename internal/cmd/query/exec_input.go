package query

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

// resolveSQL applies the documented input precedence: --sql, then
// --sql-file, then stdin (only when stdin is not a terminal). Returns a
// validated, trimmed statement.
func resolveSQL(stdin io.Reader, in execInput) (string, error) {
	switch {
	case strings.TrimSpace(in.SQL) != "":
		return validateSQL(in.SQL)
	case in.SQLFile != "":
		data, err := os.ReadFile(in.SQLFile)
		if err != nil {
			return "", fmt.Errorf("read sql file: %w", err)
		}
		return validateSQL(string(data))
	case stdinHasData(stdin):
		data, err := io.ReadAll(stdin)
		if err != nil {
			return "", fmt.Errorf("read sql from stdin: %w", err)
		}
		return validateSQL(string(data))
	default:
		return "", errors.New("no SQL provided: pass --sql, --sql-file, or pipe SQL on stdin")
	}
}

// stdinHasData reports whether the given reader is a non-TTY stream we
// should read from. The cobra default is os.Stdin, but tests inject
// strings.Reader/bytes.Buffer (which never look like a TTY); both cases
// are correct.
func stdinHasData(r io.Reader) bool {
	f, ok := r.(*os.File)
	if !ok {
		return true
	}
	stat, err := f.Stat()
	if err != nil {
		return false
	}
	return (stat.Mode() & os.ModeCharDevice) == 0
}

func validateSQL(raw string) (string, error) {
	sql := strings.TrimSpace(raw)
	sql = strings.TrimRight(sql, ";")
	sql = strings.TrimSpace(sql)
	if sql == "" {
		return "", errors.New("sql is empty")
	}
	if len(sql) > maxSQLBytes {
		return "", fmt.Errorf("sql too long: %d bytes (max %d)", len(sql), maxSQLBytes)
	}
	return sql, nil
}

func normalisePageSize(n int) (int, error) {
	if n <= 0 {
		return 0, fmt.Errorf("--limit must be positive: got %d", n)
	}
	if n > maxPageSize {
		return 0, fmt.Errorf("--limit too large: got %d (max %d)", n, maxPageSize)
	}
	return n, nil
}

// validatePageToken enforces the proto bound (max_len 100) on page tokens.
// Empty input is allowed: it means "first page".
func validatePageToken(s string) (string, error) {
	if len(s) > maxPageTokenSize {
		return "", fmt.Errorf("--page-token too long: %d bytes (max %d)", len(s), maxPageTokenSize)
	}
	return s, nil
}
