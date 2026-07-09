package main

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/ebra01/weather-tracker-v2/server/internal/weatherdb"
)

type testDBState struct {
	queryRow  []driver.Value
	queryErr  error
	queryArgs []driver.NamedValue

	execErr    error
	execCalled bool
	execArgs   []driver.NamedValue
}

func newTestQueries(t *testing.T, state *testDBState) *weatherdb.Queries {
	t.Helper()

	db := sql.OpenDB(testConnector{state: state})
	t.Cleanup(func() {
		db.Close()
	})

	return weatherdb.New(db)
}

type testConnector struct {
	state *testDBState
}

func (c testConnector) Connect(ctx context.Context) (driver.Conn, error) {
	return &testConn{state: c.state}, nil
}

func (c testConnector) Driver() driver.Driver {
	return testDriver{}
}

type testDriver struct{}

func (d testDriver) Open(name string) (driver.Conn, error) {
	return nil, errors.New("use testConnector instead")
}

type testConn struct {
	state *testDBState
}

func (c *testConn) Prepare(query string) (driver.Stmt, error) {
	return nil, errors.New("not implemented")
}

func (c *testConn) Close() error {
	return nil
}

func (c *testConn) Begin() (driver.Tx, error) {
	return nil, errors.New("not implemented")
}

func (c *testConn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	c.state.queryArgs = append([]driver.NamedValue(nil), args...)
	if c.state.queryErr != nil {
		return nil, c.state.queryErr
	}

	return &testRows{
		columns: []string{"created_at", "temperature", "humidity", "elevation"},
		row:     c.state.queryRow,
	}, nil
}

func (c *testConn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	c.state.execCalled = true
	c.state.execArgs = append([]driver.NamedValue(nil), args...)
	if c.state.execErr != nil {
		return nil, c.state.execErr
	}

	return driver.RowsAffected(1), nil
}

type testRows struct {
	columns []string
	row     []driver.Value
	sent    bool
}

func (r *testRows) Columns() []string {
	return r.columns
}

func (r *testRows) Close() error {
	return nil
}

func (r *testRows) Next(dest []driver.Value) error {
	if r.sent || r.row == nil {
		return io.EOF
	}

	copy(dest, r.row)
	r.sent = true
	return nil
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func setTestHTTPClient(t *testing.T, fn roundTripFunc) {
	t.Helper()

	oldClient := client
	client = &http.Client{Transport: fn}

	t.Cleanup(func() {
		client = oldClient
	})
}

func testHTTPResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Status:     fmt.Sprintf("%d %s", status, http.StatusText(status)),
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}
