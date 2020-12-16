package graphql

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/samsarahq/thunder/batch"
	"github.com/samsarahq/thunder/reactive"
)

func HTTPHandler(schema *Schema, middlewares ...MiddlewareFunc) http.Handler {
	return &httpHandler{
		schema:      schema,
		middlewares: middlewares,
	}
}

// HTTPHandlerWithHooks works as HTTPHandler
// but in addition provides passing errorHandler func
// which will catch errors happened outside middleware
func HTTPHandlerWithHooks(schema *Schema, finalHandler finalResponseFunc, middlewares ...MiddlewareFunc) http.Handler {
	return &httpHandler{
		schema:       schema,
		middlewares:  middlewares,
		finalHandler: finalHandler,
	}
}

type httpHandler struct {
	schema       *Schema
	finalHandler finalResponseFunc
	middlewares  []MiddlewareFunc
}

type httpPostBody struct {
	Query     string                 `json:"query"`
	Variables map[string]interface{} `json:"variables"`
}

type httpResponse struct {
	Data   interface{} `json:"data"`
	Errors interface{} `json:"errors"`
}

// SendError provides sending error message in GraphQL format. It useful in
// cases when error could be happen outside main logic for serving HTTP
// requests. The error message has the same pattern as original GraphQL response
// with HTTP 200 status code. Returns an error, that could be happened during
// sending data to client
func SendError(w http.ResponseWriter, message string) error {
	w.Header().Set("Content-Type", "application/json")
	_, err := fmt.Fprintf(w, `{"data":null,"errors":[{"message":"%s","path":null,"extensions":{"timestamp":"%s"}}]}`, message, time.Now().UTC())
	return err
}

func (h *httpHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	writeResponse := func(ctx context.Context, value interface{}, err error, query *string) {
		var errors []error
		var responseJSON []byte

		response := httpResponse{}
		if err != nil {
			errors = append(errors, err)
			response.Errors = []interface{}{newGraphQLError(err)}
		} else {
			response.Data = value
		}

		responseJSON, err = json.Marshal(response)
		if err != nil {
			errors = append(errors, err)
			err = SendError(w, newGraphQLError(err).Message)
			if err != nil {
				errors = append(errors, err)
			}
			h.finalHandler(ctx, len(responseJSON), errors, query)
			return
		}
		if w.Header().Get("Content-Type") == "" {
			w.Header().Set("Content-Type", "application/json")
		}

		_, err = w.Write(responseJSON)
		if err != nil {
			errors = append(errors, err)
		}
		h.finalHandler(ctx, len(responseJSON), errors, query)
	}

	if r.Method != "POST" {
		writeResponse(r.Context(), nil, NewClientError("request must be a POST"), nil)
		return
	}

	if r.Body == nil {
		writeResponse(r.Context(), nil, NewClientError("request must include a query"), nil)
		return
	}

	var params httpPostBody
	if err := json.NewDecoder(r.Body).Decode(&params); err != nil {
		writeResponse(r.Context(), nil, NewClientError(fmt.Sprintf("failed to recognize JSON request: '%s'", err.Error())), nil)
		return
	}

	query, err := Parse(params.Query, params.Variables)
	if err != nil {
		writeResponse(r.Context(), nil, err, &params.Query)
		return
	}

	schema := h.schema.Query
	if query.Kind == "mutation" {
		schema = h.schema.Mutation
	}
	if err := PrepareQuery(schema, query.SelectionSet); err != nil {
		writeResponse(r.Context(), nil, err, &params.Query)
		return
	}

	var wg sync.WaitGroup
	e := Executor{}

	wg.Add(1)
	runner := reactive.NewRerunner(r.Context(), func(ctx context.Context) (interface{}, error) {
		defer wg.Done()

		ctx = batch.WithBatching(ctx)

		var middlewares []MiddlewareFunc
		middlewares = append(middlewares, h.middlewares...)
		middlewares = append(middlewares, func(input *ComputationInput, next MiddlewareNextFunc) *ComputationOutput {
			output := next(input)
			output.Current, output.Error = e.Execute(input.Ctx, schema, nil, input.ParsedQuery)
			return output
		})
		input := &ComputationInput{
			Ctx:         ctx,
			ParsedQuery: query,
			Query:       params.Query,
			Variables:   params.Variables,
		}
		output := RunMiddlewares(middlewares, input)
		current, err := output.Current, output.Error

		if err != nil {
			if ErrorCause(err) != context.Canceled {
				writeResponse(input.Ctx, nil, err, &params.Query)
			}
			return nil, err
		}

		writeResponse(input.Ctx, current, nil, nil)
		return nil, nil
	}, DefaultMinRerunInterval)

	wg.Wait()
	runner.Stop()
}
