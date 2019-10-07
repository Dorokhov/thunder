package graphql_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kylelemons/godebug/pretty"

	"github.com/samsarahq/thunder/graphql"
	"github.com/samsarahq/thunder/graphql/schemabuilder"
)

func testHTTPRequest(req *http.Request) *httptest.ResponseRecorder {
	schema := schemabuilder.NewSchema()

	query := schema.Query()
	query.FieldFunc("mirror", func(args struct{ Value int64 }) int64 {
		return args.Value * -1
	})

	builtSchema := schema.MustBuild()

	rr := httptest.NewRecorder()
	finalHandler := func(responseLength int, errors []error, query *string) {}
	handler := graphql.HTTPHandlerWithHooks(builtSchema, finalHandler)

	handler.ServeHTTP(rr, req)
	return rr
}

func TestHTTPMustPost(t *testing.T) {
	req, err := http.NewRequest("GET", "/graphql", nil)
	if err != nil {
		t.Fatal(err)
	}

	rr := testHTTPRequest(req)

	if rr.Code != 200 {
		t.Errorf("expected 200, but received %d", rr.Code)
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	errors := resp["errors"].([]interface{})

	if len(errors) == 0 {
		t.Fatal("errors array is empty, when they are expected")
	} else if len(errors) != 1 {
		t.Fatal("errors has more than one value")
	}
	message := errors[0].(map[string]interface{})["message"]
	if message == nil {
		t.Error("message is not present, when it expected")
		return
	}
	if diff := pretty.Compare(message, "request must be a POST"); diff != "" {
		t.Errorf("expected response to match, but received %s", diff)
	}
}

func TestHTTPParseQuery(t *testing.T) {
	req, err := http.NewRequest("POST", "/graphql", nil)
	if err != nil {
		t.Fatal(err)
	}

	rr := testHTTPRequest(req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, but received %d", rr.Code)
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	errors := resp["errors"].([]interface{})

	if len(errors) == 0 {
		t.Fatal("errors array is empty, when they are expected")
	} else if len(errors) != 1 {
		t.Fatal("errors has more than one value")
	}
	message := errors[0].(map[string]interface{})["message"]
	if message == nil {
		t.Error("message is not present, when it expected")
	}
	if diff := pretty.Compare(message, "request must include a query"); diff != "" {
		t.Errorf("expected response to match, but received %s", diff)
	}
}

func TestHTTPMustHaveQuery(t *testing.T) {
	req, err := http.NewRequest("POST", "/graphql", strings.NewReader(`{"query":""}`))
	if err != nil {
		t.Fatal(err)
	}

	rr := testHTTPRequest(req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, but received %d", rr.Code)
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	errors := resp["errors"].([]interface{})

	if len(errors) == 0 {
		t.Fatal("errors array is empty, when they are expected")
	} else if len(errors) != 1 {
		t.Fatal("errors has more than one value")
	}
	message := errors[0].(map[string]interface{})["message"]
	if message == nil {
		t.Error("message is not present, when it expected")
	}
	if diff := pretty.Compare(message, "must have a single query"); diff != "" {
		t.Errorf("expected response to match, but received %s", diff)
	}
}

func TestHTTPSuccess(t *testing.T) {
	req, err := http.NewRequest("POST", "/graphql", strings.NewReader(`{"query": "query TestQuery($value: int64) { mirror(value: $value) }", "variables": { "value": 1 }}`))
	if err != nil {
		t.Fatal(err)
	}

	rr := testHTTPRequest(req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, but received %d", rr.Code)
	}

	if diff := pretty.Compare(rr.Body.String(), "{\"data\":{\"mirror\":-1},\"errors\":null}"); diff != "" {
		t.Errorf("expected response to match, but received %s", diff)
	}
}

func TestHTTPContentType(t *testing.T) {
	req, err := http.NewRequest("POST", "/graphql", strings.NewReader(`{"query": "query TestQuery($value: int64) { mirror(value: $value) }", "variables": { "value": 1 }}`))
	if err != nil {
		t.Fatal(err)
	}

	rr := testHTTPRequest(req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, but received %d", rr.Code)
	}

	if diff := pretty.Compare(rr.HeaderMap.Get("Content-Type"), "application/json"); diff != "" {
		t.Errorf("expected response to match, but received %s", diff)
	}
}
